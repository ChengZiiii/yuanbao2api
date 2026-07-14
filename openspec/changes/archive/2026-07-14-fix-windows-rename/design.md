# 设计：fix-windows-rename

## 架构决策

### 1. 三阶段 fallback：rename → retry → remove+rename

`atomicRename` 实现：

```go
func atomicRename(tmp, target string) error {
    // Phase 1：直接 rename。Unix 永远走这条；Windows 干净环境下也走这条。
    if err := os.Rename(tmp, target); err == nil {
        return nil
    }
    // Phase 2：短暂重试（捕获 Windows AV / 共享冲突的瞬时错误）。
    const maxAttempts = 5
    const retryDelay = 50 * time.Millisecond
    for i := 0; i < maxAttempts; i++ {
        if err := os.Rename(tmp, target); err == nil {
            return nil
        }
        time.Sleep(retryDelay)
    }
    // Phase 3：兜底 —— 显式 remove target 再 rename。
    // 牺牲严格原子性，但保证写成功。runtime_config.json 在每次保存时
    // 都是从内存完整重写，最坏后果是并发读者短暂读到旧值，
    // 文件本身不会损坏。
    if err := os.Remove(target); err != nil && !errors.Is(err, fs.ErrNotExist) {
        _ = os.Remove(tmp)
        return err
    }
    if err := os.Rename(tmp, target); err != nil {
        _ = os.Remove(tmp)
        return err
    }
    return nil
}
```

**理由**。Phase 1（Unix 路径）在干净的 Windows 上也立即成功，
不引入性能开销。Phase 2 处理 Windows 上常见的 AV 瞬时锁 —— 50ms × 5
共 250ms 的最大等待时间，对运维场景（用户手动点保存按钮）可接受。
Phase 3 是最终兜底，确保即使 AV 持续锁住也能写成功；只损失原子性的
最后一个边缘情况，而 `runtime_config.json` 在本进程内单写者，不存在
真正的并发竞争。

### 2. 错误分类

我们没有显式区分"Windows-specific Access is denied"与"其他错误"，
而是对所有 `os.Rename` 错误统一走重试 + fallback 路径。

**理由**。在 Unix 上 `os.Rename` 失败几乎都是真错（权限、跨设备、
不存在 target 之外的合理错误），而 Phase 3 的 remove+rename 在这些
错误下同样会失败并向上抛 —— 与原行为等价。在 Windows 上所有错误都
走完整三阶段，最大化成功率。代码更简单，少一组平台判断。

### 3. 不引入 syscall

考虑过用 `syscall.MoveFileEx` + `MOVEFILE_REPLACE_EXISTING` 显式调用
作为 Phase 1 的"超集"，但 `os.Rename` 在 Windows 上自 Go 1.5 起已经
这么做了，重复实现没有收益。

### 4. 测试策略

测试覆盖三个场景：

- `TestAtomicRename_TargetDoesNotExist` —— happy path，无需 fallback。
- `TestAtomicRename_TargetExists` —— 目标文件存在，触发 Phase 1 之后的
  路径。在 Windows 上即便直接 `os.Rename` 也能成功（Go 已经用
  `MOVEFILE_REPLACE_EXISTING`），但这个测试主要验证"target 已存在
  时内容被正确替换"。
- `TestAtomicRename_TmpMissing` —— 错误传播 + 不修改 target。

**不**尝试在测试里模拟 Windows AV 行为（不可移植、不可重现）。
用户层面的端到端验证在用户本地进行（保存 Cookie → 不再报
Access is denied）。

## 文件改动

### `api/config_persist.go`
- 新增 `atomicRename(tmp, target string) error`。
- `SaveRuntimeConfig` 调用点替换：`os.Rename(tmp, target)` → `atomicRename(tmp, target)`。
- 新增 import：`errors`、`io/fs`、`time`。

### `api/config_persist_test.go`
- 新增 `TestAtomicRename_TargetDoesNotExist`。
- 新增 `TestAtomicRename_TargetExists`。
- 新增 `TestAtomicRename_TmpMissing`。

## 数据流

```
HandleSetConfig 收到 POST /api/config
   │
   ▼
SaveRuntimeConfig(cfg)
   │ os.WriteFile(tmp, data, 0600)  // 写 .tmp
   │ atomicRename(tmp, target)
   │      Phase 1: os.Rename
   │          ├─ Unix：成功返回 nil
   │          └─ Windows（AV 锁）：失败 → 进入 Phase 2
   │      Phase 2: 短暂重试（≤5 × 50ms）
   │          ├─ AV 锁松开：成功
   │          └─ 仍失败：进入 Phase 3
   │      Phase 3: os.Remove(target) + os.Rename(tmp, target)
   │          ├─ 成功
   │          └─ 仍失败：清 tmp、返回错误
   ▼
runtime_config.json 在磁盘上被原子替换为新内容
```

## 失败模式与对策

- **AV 在 Phase 2 期间持续锁住、超时仍失败** → Phase 3 兜底；
  即便 AV 永远锁，最终 `os.Remove(target)` 也会因 AV 拦截失败，
  此时向上抛错（与原行为一致 —— 用户必须解决 AV 拦截才能写入）。
- **磁盘满 / 权限错误** → Phase 1 即失败，Phase 2 重试也失败，
  Phase 3 的 Remove 也失败 → 向上抛错。不引入新的失败模式。
- **target 不在同卷** → `os.Rename` 在 Unix 上失败，
  Phase 3 兜底。但 `tmp = target + ".tmp"` 保证两者同目录同卷，
  实际不会触发。
- **tmp 文件在保存中途被其他进程删掉** → Phase 1 失败、Phase 2 重试
  全部失败（因为 tmp 不存在）、Phase 3 Remove target 成功但
  Rename(tmp, target) 因 tmp 不存在失败 → 向上抛 `os.Rename` 错误。
  `SaveRuntimeConfig` 的 `os.WriteFile` 错误也会被向上抛。

## 性能影响

- Phase 1 成功路径：与原实现完全一致（一次 `os.Rename`）。
- Phase 2 路径：≤ 250ms 阻塞等待；对用户操作（点按钮）可接受。
- Phase 3 路径：增加一次 `os.Remove` + 一次 `os.Rename`。
  罕见路径，开销可忽略。
# 提案：fix-windows-rename

## Why

`api/config_persist.go` 的 `SaveRuntimeConfig` 在 Windows 环境下写入
`runtime_config.json` 时报：

```
rename ./runtime_config.json.tmp ./runtime_config.json: Access is denied.
```

触发场景：用户在面板点"保存 Cookie"（或任意 `POST /api/config` 携带
运行时字段）→ `HandleSetConfig` 调用 `SaveRuntimeConfig` →
`os.Rename(tmp, target)` 被 Windows 拒绝。

根因：尽管 Go 1.5+ 的 `os.Rename` 在 Windows 上会调用
`MoveFileEx(... MOVEFILE_REPLACE_EXISTING)`，在某些环境（防病毒软件
正在扫描 `runtime_config.json`、FAT 卷、残留句柄）下仍会返回
`Access is denied`。

为什么之前没暴露：`runtime-cookie` 之前的 `SaveRuntimeConfig` 调用面
较窄（仅 `maxConcurrency` / `queueTimeoutSeconds` / `requestCooldownMs`
变化时才触发），用户少碰；`runtime-cookie` 把"保存 Cookie"接入
同一条写路径后，每次保存 Cookie 都会写文件，触发频率显著上升。

## What Changes

- 在 `api/config_persist.go` 引入 `atomicRename(tmp, target string) error`
  助手：
  1. 优先尝试 `os.Rename(tmp, target)`（Unix 行为，无需任何 fallback）。
  2. 失败时若错误是 Windows 上常见的"Access is denied / sharing
     violation"，重试若干次（短暂 backoff）。
  3. 重试用尽仍失败 → 兜底 `os.Remove(target)` + `os.Rename(tmp, target)`
     （牺牲严格的原子性，但保证写成功；`runtime_config.json` 在每次保存时
     都是从内存状态完整重写，失败的最坏后果是并发读者读到旧值，文件不会损坏）。
  4. 其他类型错误（如临时文件 `os.WriteFile` 失败）原样返回。
- `SaveRuntimeConfig` 改用 `atomicRename`。
- 增加单元测试：target 不存在、target 已存在、tmp 不存在三类场景。
  target 已存在场景在 Windows CI / 本机环境下能可靠触发 fallback 路径。

## Impact

- 受影响 spec：`configuration`（本 change 修改既有行为，向其新增一条
  `MODIFIED Requirements`）。
- 受影响文件：`api/config_persist.go`、`api/config_persist_test.go`。
  仅 1 个生产文件 + 1 个测试文件。
- 不引入新依赖。
- 不影响 `runtime-cookie` 已归档的需求：Cookie 解析优先级、保存/清除/
  拒绝路径完全不变。

## Compatibility

- Unix / Linux / macOS：行为完全不变 —— 第一步 `os.Rename` 永远成功，
  fallback 永远不会执行。
- Windows（未触发 AV 拦截）：行为完全不变 —— 第一步 `os.Rename` 成功。
- Windows（触发 AV 拦截）：从"100% 失败"变为"成功（带短暂重试）"。
- 文件最终内容：在所有路径下都与"内存状态完整重写"一致，与当前
  `SaveRuntimeConfig` 在成功时的输出字节相同。

## Approach（高层）

1. `api/config_persist.go`：
   - 新增 `atomicRename`（package-private helper）。
   - `SaveRuntimeConfig` 把单行 `os.Rename(tmp, target)` 替换为
     `atomicRename(tmp, target)`。
2. `api/config_persist_test.go`：
   - 新增 `TestAtomicRename_TargetDoesNotExist`（happy path）。
   - 新增 `TestAtomicRename_TargetExists`（Windows 下走 fallback 路径）。
   - 新增 `TestAtomicRename_TmpMissing`（错误传播）。
# 任务：fix-windows-rename

实施清单。1 个生产文件 + 1 个测试文件，约 30 行新增。

## 1. 原子 rename 助手

- [x] 1.1 在 `api/config_persist.go` 新增 `atomicRename(tmp, target string) error`：
      - Phase 1：`os.Rename(tmp, target)`，成功立即返回 nil。
      - Phase 2：失败时短暂重试（≤5 次 × 50ms）。
      - Phase 3：兜底 `os.Remove(target)` + `os.Rename(tmp, target)`。
      - 全程清理 tmp。
- [x] 1.2 `SaveRuntimeConfig` 把单行 `os.Rename(tmp, target)` 替换为
      `atomicRename(tmp, target)`。
- [x] 1.3 新增 import：`errors`、`io/fs`、`time`。

## 2. 测试

- [x] 2.1 在 `api/config_persist_test.go` 新增
      `TestAtomicRename_TargetDoesNotExist`：tmp 存在、target 不存在，
      调用 atomicRename 后 target 存在且内容 == tmp 内容。
- [x] 2.2 新增 `TestAtomicRename_TargetExists`：tmp 存在、target 已存在
      （旧内容与新内容不同），调用 atomicRename 后 target 内容
      等于 tmp 内容，旧内容被覆盖。
- [x] 2.3 新增 `TestAtomicRename_TmpMissing`：tmp 不存在，
      atomicRename 返回非 nil 错误，target 文件不被修改。

## 3. 验证

- [x] 3.1 `go build ./...` 通过。
- [x] 3.2 `go test ./... -count=1` 全部通过（包括 3 个新测试 + 既有
      `runtime_config` 持久化测试 + `HandleSetConfig` cookie 测试）。
- [x] 3.3 `go vet ./...` 无告警。
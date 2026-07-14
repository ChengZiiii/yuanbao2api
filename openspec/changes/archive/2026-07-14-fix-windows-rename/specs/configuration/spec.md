# Spec: configuration（增量）

本 change 是 `configuration` 领域的 `MODIFIED` delta —— 修改既有
"SaveRuntimeConfig 写入"行为的实现细节（不修改外部可观察行为）。

## MODIFIED Requirements

### Requirement: Cookie 持久化至 runtime_config.json

> 修改范围：本 Requirement 的实现侧 —— `SaveRuntimeConfig` 的落盘方式。
> 外部可观察行为（持久化往返、向后兼容加载）保持不变；新增 Windows
> 下"原子 rename 在 AV / 共享冲突下最终成功"的实现契约。

系统 SHALL 将元宝上游 Cookie 持久化到 `runtime_config.json` 的
`yuanbaoCookie` 字段，以便运维能在不编辑 `.env` 的前提下更新 Cookie。

Cookie SHALL 以原子方式写入，文件权限为 `0600`，与现有
`SaveRuntimeConfig` 的写入语义保持一致。该字段 SHALL 可选（可缺省）：
缺省即代表"无运行时覆盖"，系统从而回落至环境变量。

`SaveRuntimeConfig` SHALL 在 Windows 上使用一个**对 AV / 共享冲突友好的
原子 rename 路径**：优先 `os.Rename`，失败时短暂重试，必要时显式
`os.Remove` 旧 target 后再 rename —— 保证 `Access is denied` 类错误
最终能成功。文件最终内容 SHALL 与"从内存完整重写"语义一致；
fallback 路径不得引入新的失败模式（最坏后果是并发读者短暂读到旧值，
不得损坏 JSON 文件或泄露 tmp 文件）。

#### Scenario: 持久化往返

- **WHEN** 运维经 `POST /api/config { yuanbaoCookie: "abc..." }` 保存 Cookie
- **THEN** 磁盘上 `runtime_config.json` 包含 `"yuanbaoCookie": "abc..."`
- **AND** 重启后 `LoadRuntimeConfig()` 返回的 `RuntimeConfig`
  其 `YuanbaoCookie` 字段被设为 `"abc..."`

#### Scenario: 向后兼容加载

- **WHEN** `runtime_config.json` 已存在但不含 `yuanbaoCookie` 字段
  （例如本 change 之前生成的文件）
- **THEN** `LoadRuntimeConfig()` 返回的 `RuntimeConfig` 其
  `YuanbaoCookie == nil`
- **AND** Cookie 解析逻辑回落至 env 值

#### Scenario: 目标不存在（Unix / 干净 Windows）

- **WHEN** 目标 `runtime_config.json` 不存在，tmp 文件存在
- **THEN** `SaveRuntimeConfig` 调用 `os.Rename(tmp, target)` 一次成功
- **AND** 目标文件内容等于内存中 `RuntimeConfig` 的 JSON 序列化结果

#### Scenario: 目标已存在（Windows AV 拦截场景）

- **GIVEN** 目标 `runtime_config.json` 已存在且 `os.Rename(tmp, target)`
  因 Windows `Access is denied` 失败若干次
- **WHEN** 调用 `SaveRuntimeConfig`
- **THEN** 实现最终成功替换目标文件
- **AND** 目标文件最终内容等于内存中 `RuntimeConfig` 的 JSON 序列化结果
- **AND** 不遗留 `.tmp` 文件

#### Scenario: tmp 缺失（前置失败）

- **GIVEN** tmp 文件已被前序失败清理（`os.WriteFile` 失败）
- **WHEN** 调用 `SaveRuntimeConfig`
- **THEN** 返回错误，且不修改目标文件
- **AND** 不泄露任何 `.tmp` 文件

#### Scenario: 旧行为回归保护

- **WHEN** 在 Unix / Linux / macOS 或未被 AV 拦截的 Windows 上调用
  `SaveRuntimeConfig`
- **THEN** 第一次 `os.Rename` 即成功，**fallback 路径不被触发**
- **AND** 性能开销在毫秒级以内（与改造前一致）

## ADDED Requirements

### Requirement: 原子 rename 助手 internal 契约

> 新增 helper `atomicRename(tmp, target string) error` 的内部契约。
> 该 helper 是 `SaveRuntimeConfig` 的内部实现细节，不暴露给外部
> package，因此本 Requirement 描述其**实现层契约**，非 API 契约。

`atomicRename` SHALL：
- 接受 tmp 与 target 两个路径字符串。
- 在 Unix / Linux / macOS 上 SHALL 等价于一次 `os.Rename(tmp, target)`。
- 在 Windows 上 SHALL 实现"重试 → 必要时 fallback 到 remove+rename"
  的语义，使得 `Access is denied` 类错误最终能成功。
- 不得泄露 `.tmp` 文件：成功或最终失败都必须清理 tmp。
- SHALL 不修改 target 文件的内容（写入内容由调用方决定）。

#### Scenario: 原子 rename 成功

- **WHEN** target 不存在或可被 rename 覆盖
- **THEN** `atomicRename` 返回 nil
- **AND** target 文件存在且内容等于 tmp 内容

#### Scenario: 原子 rename 在 Windows 上重试

- **GIVEN** 模拟首次 `os.Rename` 返回 `Access is denied` 错误
  （通过文件系统使目标被锁，或在测试中 stub）
- **WHEN** 调用 `atomicRename`
- **THEN** helper 在短暂重试或 fallback 后最终成功
- **AND** 不向上抛出原始错误

#### Scenario: 原子 rename 错误传播

- **GIVEN** tmp 不存在
- **WHEN** 调用 `atomicRename`
- **THEN** 返回非 nil 错误
- **AND** target 文件不被修改
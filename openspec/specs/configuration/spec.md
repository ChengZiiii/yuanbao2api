# configuration Specification

## Purpose
TBD - created by archiving change runtime-cookie. Update Purpose after archive.
## Requirements
### Requirement: Cookie 持久化至 runtime_config.json

> 修改范围：数据模型。Cookie 在磁盘上的 JSON 形态从
> `"yuanbaoCookie": "hy_token=...; hy_user=..."`（字符串）
> 改为 `"yuanbaoCookie": {"hyToken":"...","hyUser":"..."}`（对象）。
> 写入语义（原子性、`0600` 权限、Windows rename 行为）保持不变
> （已由 `fix-windows-rename` 锁定）。

系统 SHALL 将元宝上游 Cookie 持久化到 `runtime_config.json` 的
`yuanbaoCookie` 字段，类型为对象 `{hyToken, hyUser}`，以便运维能在
不编辑 `.env` 的前提下更新 Cookie，且无需手敲 `hy_token=` / `hy_user=`
字段名。

Cookie SHALL 以原子方式写入，文件权限为 `0600`，与现有
`SaveRuntimeConfig` 的写入语义保持一致。`yuanbaoCookie` SHALL 可选
（可缺省）：缺省即代表"无运行时覆盖"，系统从而回落至环境变量。

为支持从 `runtime-cookie` 期间的中间数据格式迁移，
`YuanbaoCookie` 类型 SHALL 同时接受 JSON 字符串形式
（`"hy_token=xxx; hy_user=yyy"`）作为反序列化输入；其 SHALL 被解析为
对应字段。序列化输出 SHALL 始终是对象形式。

#### Scenario: 持久化往返

- **WHEN** 运维经 `POST /api/config { yuanbaoCookie: { hyToken: "abc", hyUser: "xyz" } }` 保存 Cookie
- **THEN** 磁盘上 `runtime_config.json` 包含
  `"yuanbaoCookie": {"hyToken":"abc","hyUser":"xyz"}`
- **AND** 重启后 `LoadRuntimeConfig()` 返回的 `RuntimeConfig`
  其 `YuanbaoCookie` 字段 `HyToken == "abc"` 且 `HyUser == "xyz"`

#### Scenario: 向后兼容加载

- **WHEN** `runtime_config.json` 已存在但不含 `yuanbaoCookie` 字段
  （例如本 change 之前生成的文件）
- **THEN** `LoadRuntimeConfig()` 返回的 `RuntimeConfig` 其
  `YuanbaoCookie == nil`
- **AND** Cookie 解析逻辑回落至 env 值

#### Scenario: 旧字符串形式自动迁移

- **WHEN** `runtime_config.json` 已存在且
  `yuanbaoCookie` 为字符串 `"hy_token=legacy; hy_user=old"`
  （`runtime-cookie` 期间可能落地的格式）
- **THEN** `LoadRuntimeConfig()` 返回的 `RuntimeConfig`
  其 `YuanbaoCookie` 字段 `HyToken == "legacy"` 且 `HyUser == "old"`
- **AND** Cookie 解析逻辑按新数据模型工作

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

### Requirement: Cookie 解析优先级

> 修改范围：`EffectiveYuanbaoCookie()` 的内部实现。它仍然返回 HTTP
> `Cookie` 头字符串（拼装自 `HyToken` 与 `HyUser`），外部契约不变。

系统 SHALL 在每次发起上游请求时（而非在客户端构造时）解析元宝上游 Cookie，
按以下优先级：

1. `runtime_config.json:yuanbaoCookie`（已设且 `HyToken` 或 `HyUser`
   任一非空时）
2. `YUANBAO_COOKIE` 环境变量（已设且非空时）—— 作为整段 Cookie 头
   字符串原样使用
3. 空字符串（调用方按"无 Cookie"处理）

解析逻辑 SHALL 封装在单一助手（`EffectiveYuanbaoCookie()`）中，
所有上游调用方均通过该助手读取，禁止直接读 env。

当优先级 1 命中且 `YuanbaoCookie` 不为 nil 时，组装 SHALL 按
`hy_token=<HyToken>; hy_user=<HyUser>` 格式进行；任一字段为空时
对应段 SHALL 省略。

#### Scenario: 运行时覆盖 env

- **GIVEN** `runtime_config.json` 中
  `yuanbaoCookie = {"hyToken":"runtime-tok","hyUser":"runtime-usr"}`
  **AND** env `YUANBAO_COOKIE` 为 `"env-cookie"`
- **WHEN** 上游客户端发起请求
- **THEN** `Cookie` 请求头为 `"hy_token=runtime-tok; hy_user=runtime-usr"`

#### Scenario: env 兜底

- **GIVEN** `runtime_config.json` 中无 `yuanbaoCookie` 字段
  **AND** env `YUANBAO_COOKIE` 为 `"env-cookie"`
- **WHEN** 上游客户端发起请求
- **THEN** `Cookie` 请求头为 `"env-cookie"`

#### Scenario: 两者皆空

- **GIVEN** 运行时 `YuanbaoCookie == nil` **AND** env 无 Cookie
- **WHEN** 上游客户端发起请求
- **THEN** 出站请求不设置 `Cookie` 请求头
- **AND** 上游按预期返回 4xx，由 API 层以普通上游错误形式回传调用方

### Requirement: 经管理 API 更新 Cookie

> 修改范围：请求体形态。`yuanbaoCookie` 字段从字符串改为对象。
> 校验与清空/缺省语义保持不变。

`POST /api/config` SHALL 接受可选的 `yuanbaoCookie` **对象**
字段，结构为 `{hyToken: string, hyUser: string}`。出现该字段时：

- `yuanbaoCookie` 必须为 JSON object，否则请求 SHALL 被拒，HTTP 400。
- 若对象两字段都为空字符串或对象两字段都缺省，运行时覆盖 SHALL
  被清除（`YuanbaoCookie` 设为 `nil` 并从 `runtime_config.json` 移除）。
- 否则运行时 SHALL 持久化该对象（仅写入提供的字段；缺省字段保留
  为空字符串）。

端点 SHALL 继续沿用现有的部分更新规则：`yuanbaoCookie` 字段在请求体
中缺省时 MUST NOT 清零既有值（与既有 `deepThinking` 等字段一致）。

#### Scenario: 保存非空 Cookie

- **WHEN** `POST /api/config` 收到
  `{ "yuanbaoCookie": { "hyToken": "abc", "hyUser": "xyz" } }`
- **THEN** 响应为 HTTP 200，并附最新服务端配置
- **AND** `runtime_config.json` 包含
  `"yuanbaoCookie": {"hyToken":"abc","hyUser":"xyz"}`

#### Scenario: 用空字符串清除运行时 Cookie

- **WHEN** `POST /api/config` 收到 `{ "yuanbaoCookie": {} }` 且当前
  已持久化有运行时 Cookie（请求体中的对象两字段都为空或都缺省）
- **THEN** 响应为 HTTP 200
- **AND** 下次 `LoadRuntimeConfig()` 返回 `YuanbaoCookie == nil`
- **AND** 后续上游请求从 env 解析 Cookie

#### Scenario: 缺省字段为 no-op

- **WHEN** `POST /api/config` 收到 `{ "deepThinking": true }`，无
  `yuanbaoCookie` 字段
- **THEN** 既有运行时 Cookie（如有）保持不变
- **AND** 不会以清空 Cookie 的方式重写 `runtime_config.json`

#### Scenario: 类型错误

- **WHEN** `POST /api/config` 收到 `{ "yuanbaoCookie": "not-an-object" }`
- **THEN** 响应为 HTTP 400，并附说明性错误信息

### Requirement: 经 /api/env 暴露 Cookie 来源

> 修改范围：响应体增加分项掩码字段，旧的 `yuanbaoCookie` 字段保留。

`GET /api/env` SHALL 继续把 Cookie 打码为前 8 位 + `****`，
同时 SHALL 包含 `cookieSource` 字段（值同前：`"runtime"` / `"env"` /
`"none"`）。

此外 SHALL 包含：
- `yuanbaoHyToken`：当前生效 Cookie 中 `hy_token` 部分的前 8 位 +
  `****`；空时为 `""`。
- `yuanbaoHyUser`：当前生效 Cookie 中 `hy_user` 部分的前 8 位 +
  `****`；空时为 `""`。
- `yuanbaoCookie`：保持既有字段，拼装后字符串的前 8 位 + `****`；
  空时为 `""`。

> 旧 `yuanbaoCookie` 字段保留是为了不破坏可能依赖它的高级客户端。

#### Scenario: 显示来源

- **WHEN** runtime `YuanbaoCookie` 为 `{"hyToken":"abcdef1234","hyUser":"uvwxyz5678"}`，
  env Cookie 未设
- **THEN** `GET /api/env` 返回
  `{ "yuanbaoCookie": "hy_toke****", "yuanbaoHyToken": "abcdef1****", "yuanbaoHyUser": "uvwxyz****", "cookieSource": "runtime", ... }`

#### Scenario: env 兜底报告 env 来源

- **WHEN** 运行时 Cookie 未设，env Cookie 已设
- **THEN** `GET /api/env` 返回 `"cookieSource": "env"`

### Requirement: 面板 Cookie 编辑入口

> 修改范围：UI 由单个 textarea 改为两个独立 input。

管理面板的配置页 SHALL 提供**两个独立输入框**用于 Cookie：

- 一个 `id="yuanbaoHyTokenInput"` 用于填 `hy_token` 的值；
- 一个 `id="yuanbaoHyUserInput"` 用于填 `hy_user` 的值；
- 一个 "保存 Cookie" 按钮，点击后 POST
  `{ yuanbaoCookie: { hyToken: <value>, hyUser: <value> } }` 到
  `/api/config`。

两个输入框 SHALL 默认隐藏（带显隐切换），以避免明文暴露在屏幕上。
每个输入框 SHALL 在 `GET /api/env` 返回时被预填充为对应字段的掩码值，
以便运维能在不清空的情况下编辑单个字段。

保存成功后，面板 SHALL 展示"需重启生效"提示（与并发保存一致），
引导用户点击现有的"🔄 重启服务"按钮。

#### Scenario: 运维经面板更新 Cookie

- **GIVEN** 运维已在两个输入框分别填好新值
- **WHEN** 运维点击"保存 Cookie"
- **THEN** 面板向 `/api/config` 发起
  `{ yuanbaoCookie: { hyToken: <tok>, hyUser: <usr> } }` 的 POST
- **AND** HTTP 200 时，面板显示"已保存。点击'重启服务'按钮生效。"
- **AND** HTTP 非 200 时，面板显示 API 返回的错误信息

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

### Requirement: Cookie 头拼装

当 `EffectiveYuanbaoCookie()` 在运行时命中优先级 1（即 `YuanbaoCookie`
非 nil 且至少一字段非空）时，组装 SHALL 按以下规则：

- 若 `HyToken` 非空，包含段 `hy_token=<HyToken>`。
- 若 `HyUser` 非空，包含段 `hy_user=<HyUser>`。
- 多段以 `"; "` 连接；段间顺序 SHALL 固定为 `hy_token` 在前、`hy_user`
  在后（与 Yuanbao 服务端期望一致；如有协议变更需另外走 change）。

`YuanbaoCookie` SHALL 提供 `HeaderValue() string` 方法实现上述组装，
供 `EffectiveYuanbaoCookie()` 调用。

#### Scenario: 双字段组装

- **WHEN** `YuanbaoCookie{HyToken:"abc", HyUser:"xyz"}`
- **THEN** `HeaderValue()` 返回 `"hy_token=abc; hy_user=xyz"`

#### Scenario: 仅 Token 非空

- **WHEN** `YuanbaoCookie{HyToken:"abc", HyUser:""}`
- **THEN** `HeaderValue()` 返回 `"hy_token=abc"`

#### Scenario: 仅 User 非空

- **WHEN** `YuanbaoCookie{HyToken:"", HyUser:"xyz"}`
- **THEN** `HeaderValue()` 返回 `"hy_user=xyz"`

#### Scenario: 全空

- **WHEN** `YuanbaoCookie{HyToken:"", HyUser:""}`
- **THEN** `HeaderValue()` 返回 `""`


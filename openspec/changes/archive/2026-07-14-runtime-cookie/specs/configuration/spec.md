# Spec: configuration（配置）

本 change 的 delta 规格。`configuration` 是新建领域；archive 阶段
`libretto-sync-specs` 会把它合并进 `openspec/specs/configuration/spec.md`。

> 说明：OpenSpec 解析器对结构性段头（`## ADDED Requirements`、
> `### Requirement:`、`#### Scenario:`）按固定字符串匹配，故此处保留
> 英文标记，标题与正文以中文书写。

## ADDED Requirements

### Requirement: Cookie 持久化至 runtime_config.json

系统 SHALL 将元宝上游 Cookie 持久化到 `runtime_config.json` 的
`yuanbaoCookie` 字段，以便运维能在不编辑 `.env` 的前提下更新 Cookie。

Cookie SHALL 以原子方式写入，文件权限为 `0600`，与现有
`SaveRuntimeConfig` 的写入语义保持一致。该字段 SHALL 可选（可缺省）：
缺省即代表"无运行时覆盖"，系统从而回落至环境变量。

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

### Requirement: Cookie 解析优先级

系统 SHALL 在每次发起上游请求时（而非在客户端构造时）解析元宝上游 Cookie，
按以下优先级：

1. `runtime_config.json:yuanbaoCookie`（已设且非空时）
2. `YUANBAO_COOKIE` 环境变量（已设且非空时）
3. 空字符串（调用方按"无 Cookie"处理）

解析逻辑 SHALL 封装在单一助手（`EffectiveYuanbaoCookie()`）中，
所有上游调用方均通过该助手读取，禁止直接读 env。

#### Scenario: 运行时覆盖 env

- **GIVEN** `runtime_config.json` 中 `yuanbaoCookie = "runtime-cookie"`
  **AND** env `YUANBAO_COOKIE` 为 `"env-cookie"`
- **WHEN** 上游客户端发起请求
- **THEN** `Cookie` 请求头为 `"runtime-cookie"`

#### Scenario: env 兜底

- **GIVEN** `runtime_config.json` 中无 `yuanbaoCookie` 字段
  **AND** env `YUANBAO_COOKIE` 为 `"env-cookie"`
- **WHEN** 上游客户端发起请求
- **THEN** `Cookie` 请求头为 `"env-cookie"`

#### Scenario: 两者皆空

- **GIVEN** 运行时无 Cookie **AND** env 无 Cookie
- **WHEN** 上游客户端发起请求
- **THEN** 出站请求不设置 `Cookie` 请求头
- **AND** 上游按预期返回 4xx，由 API 层以普通上游错误形式回传调用方

### Requirement: 经管理 API 更新 Cookie

`POST /api/config` SHALL 接受可选的 `yuanbaoCookie` 字符串字段。
出现该字段时：

- 非空字符串 SHALL 持久化至 `runtime_config.json`，并写入内存
  `ServerConfigData.YuanbaoCookie`。
- 空字符串 SHALL 清除运行时覆盖（`YuanbaoCookie` 在内存中设为 `nil`，
  下次保存时从 `runtime_config.json` 中移除），后续请求回落至 env。
- 当 `yuanbaoCookie` 出现但不是字符串时，请求 SHALL 被拒，HTTP 400。

端点 SHALL 继续沿用现有的部分更新规则：缺省字段 MUST NOT 清零既有值。

#### Scenario: 保存非空 Cookie

- **WHEN** `POST /api/config` 收到 `{ "yuanbaoCookie": "abc..." }`
- **THEN** 响应为 HTTP 200，并附最新服务端配置
- **AND** `runtime_config.json` 包含 `"yuanbaoCookie": "abc..."`

#### Scenario: 用空字符串清除运行时 Cookie

- **WHEN** `POST /api/config` 收到 `{ "yuanbaoCookie": "" }` 且当前
  已持久化有运行时 Cookie
- **THEN** 响应为 HTTP 200
- **AND** 下次 `LoadRuntimeConfig()` 返回 `YuanbaoCookie == nil`
- **AND** 后续上游请求从 env 解析 Cookie

#### Scenario: 缺省字段为 no-op

- **WHEN** `POST /api/config` 收到 `{ "deepThinking": true }`，无
  `yuanbaoCookie` 字段
- **THEN** 既有运行时 Cookie（如有）保持不变
- **AND** 不会以清空 Cookie 的方式重写 `runtime_config.json`

#### Scenario: 类型错误

- **WHEN** `POST /api/config` 收到 `{ "yuanbaoCookie": 123 }`
- **THEN** 响应为 HTTP 400，并附说明性错误信息

### Requirement: 经 /api/env 暴露 Cookie 来源

`GET /api/env` SHALL 继续把 Cookie 打码为前 8 位 + `****`。
此外 SHALL 包含 `cookieSource` 字段，取值为下列之一：

- `"runtime"` —— 当前 Cookie 来自 `runtime_config.json`
- `"env"` —— 当前 Cookie 来自 `YUANBAO_COOKIE`
- `"none"` —— 两者皆未设置

#### Scenario: 显示来源

- **WHEN** 运行时 Cookie 为 `"abcdef123456..."`，env Cookie 未设
- **THEN** `GET /api/env` 返回
  `{ "yuanbaoCookie": "abcdef12****", "cookieSource": "runtime", ... }`

#### Scenario: env 兜底报告 env 来源

- **WHEN** 运行时 Cookie 未设，env Cookie 已设
- **THEN** `GET /api/env` 返回 `"cookieSource": "env"`

### Requirement: 面板 Cookie 编辑入口

管理面板的配置页 SHALL 提供多行 Cookie 输入框与"保存 Cookie"按钮。

- 输入框 SHALL 默认隐藏（type="password" 或带显隐切换），避免明文
  暴露在屏幕上。
- 按钮 SHALL 向 `/api/config` 发起 `{ "yuanbaoCookie": <value> }` 的 POST。
- 保存成功后，面板 SHALL 展示"需重启生效"提示（与并发保存一致），
  引导用户点击现有的"🔄 重启服务"按钮。
- 现有的"环境变量一览"中 `YUANBAO_COOKIE` 行 SHALL 显示
  `GET /api/env` 返回的 `cookieSource`。

#### Scenario: 运维经面板更新 Cookie

- **GIVEN** 运维已在面板输入框粘贴新 Cookie
- **WHEN** 运维点击"保存 Cookie"
- **THEN** 面板向 `/api/config` 发起带新 Cookie 的 POST
- **AND** HTTP 200 时，面板显示"已保存。点击'重启服务'按钮生效。"
- **AND** HTTP 非 200 时，面板显示 API 返回的错误信息
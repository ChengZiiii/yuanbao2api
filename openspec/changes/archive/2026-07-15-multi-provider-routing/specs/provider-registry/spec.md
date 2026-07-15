# Spec: provider-registry（新领域）

> 由 `multi-provider-routing` 引入。本文件是合并后的主 spec。
> 工件 `openspec/changes/multi-provider-routing/specs/provider-registry/spec.md`
> 中包含本领域的全部 ADDED Requirements；归档时会被合并到
> `openspec/specs/provider-registry/spec.md`。

## ADDED Requirements

### Requirement: Provider 接口

`Provider` 接口 SHALL 描述任何上游 web2api 必须实现的方法集。

每个 provider SHALL 实现：
- `Name() string` —— provider 标识（`"yuanbao"` / `"qwen"` / `"kimi"`）。
- `Models() []ModelInfo` —— 该 provider 官方支持的模型列表。
- `BuildPrompt(messages, tools) (prompt, toolSystem, err)` —— OpenAI/
  Anthropic 风格消息 → provider 内部 prompt 字符串。
- `NewRequest(prompt, opts) (any, error)` —— 构造 provider 特定的
  请求体（any 因为各 provider 类型不同）。
- `Send(req, agentID, conversationID) (*http.Response, error)` —— 发送
  并返回原始 HTTP 响应。
- `ParseStreamLine(line) (*StreamChunk, error)` —— SSE 单行解析。

#### Scenario: yuanbao 实现 Provider

- **WHEN** `providers/yuanbao/provider.go` 实例化
- **THEN** 其 `Name() = "yuanbao"`，`Models()` 至少包含 `deep_seek_v3` 与 `hunyuan`
- **AND** `BuildPrompt` / `NewRequest` / `Send` / `ParseStreamLine` 在正常路径下可用

#### Scenario: qwen 占位实现 Provider

- **WHEN** `providers/qwen/provider.go` 实例化
- **THEN** 其 `Name() = "qwen"`，`Models()` 至少包含 `qwen-max`、`qwen-plus`
- **AND** `BuildPrompt` / `NewRequest` / `Send` 全部返回
  `"qwen provider is not yet implemented"` 错误
- **AND** `ParseStreamLine` 始终返回 `nil, nil`

#### Scenario: kimi 占位实现 Provider

- **WHEN** `providers/kimi/provider.go` 实例化
- **THEN** 其 `Name() = "kimi"`，`Models()` 至少包含 `kimi-k2`、`moonshot-v1-128k`
- **AND** `BuildPrompt` / `NewRequest` / `Send` 全部返回
  `"kimi provider is not yet implemented"` 错误
- **AND** `ParseStreamLine` 始终返回 `nil, nil`

### Requirement: Registry 模型路由

`Registry` SHALL 根据 `model` 名把请求路由到对应 provider。

路由算法：
1. 在 `defaultProvider` 的 `Models()` 中查找 `model`；命中则返回该 provider。
2. 否则遍历 `Registry.All()` 中其它 provider 的 `Models()`；命中则返回。
3. 都不命中 → 返回 `(nil, "unknown model: <name>")`。
4. 命中但 provider `enabled == false` → 返回 `(nil, "provider disabled: <name>")`。

#### Scenario: 默认 provider 命中

- **GIVEN** `defaultProvider = "yuanbao"` 且 yuanbao 已启用
- **WHEN** `Registry.Route("deep_seek_v3")` 被调用
- **THEN** 返回 yuanbao provider 实例，无 error

#### Scenario: 跨 provider 命中

- **GIVEN** qwen provider 已启用
- **WHEN** `Registry.Route("qwen-max")` 被调用
- **THEN** 返回 qwen provider 实例，无 error

#### Scenario: 未知 model

- **WHEN** `Registry.Route("nonexistent-model")` 被调用
- **THEN** 返回 `(nil, error)`，error 包含 "unknown model"

#### Scenario: 命中但 provider 停用

- **GIVEN** qwen provider `enabled == false`
- **WHEN** `Registry.Route("qwen-max")` 被调用
- **THEN** 返回 `(nil, error)`，error 包含 "provider disabled"

#### Scenario: 命中占位 provider（qwen/kimi）

- **GIVEN** qwen 已启用但是占位
- **WHEN** `Registry.Route("qwen-max")` 被调用
- **THEN** 返回 qwen provider 实例（路由成功）；调用方
  `BuildPrompt` / `Send` 时才会收到 "not implemented" 错误
- 解释：Registry 只做"是否存在 / 是否启用"判断，**是否真实可用**
  是 provider 自己的责任

### Requirement: StreamChunk 统一结构

所有 provider 的流式响应 SHALL 被解析为统一的 `StreamChunk`：
- `Type == "think"` → `Content` 字段是思考内容
- `Type == "text"` → `Text` 字段是正文内容
- 其它 `Type` SHALL 被 handler 当作未知忽略

#### Scenario: yuanbao think chunk

- **WHEN** `yuanbao.ParseStreamLine` 解析一行 `data: {"type":"think","content":"...思考..."}`
- **THEN** 返回 `&StreamChunk{Type:"think", Content:"...思考..."}`

#### Scenario: yuanbao text chunk

- **WHEN** `yuanbao.ParseStreamLine` 解析一行 `data: {"type":"text","msg":"...正文..."}`
- **THEN** 返回 `&StreamChunk{Type:"text", Text:"...正文..."}`

#### Scenario: qwen / kimi 解析（占位）

- **WHEN** `qwen.ParseStreamLine` / `kimi.ParseStreamLine` 解析任意输入
- **THEN** 始终返回 `(nil, nil)`

### Requirement: 站点管理面板 tab

管理面板 SHALL 新增"站点管理"tab（在 dashboard / testing / config /
info 之后），提供按 provider 的编辑 UI。

#### Scenario: 列出所有 provider

- **GIVEN** registry 含 yuanbao、qwen、kimi
- **WHEN** 运维切到"站点管理"tab
- **THEN** 面板渲染 3 个 provider 折叠面板（yuanbao、qwen、kimi），
  每个显示状态徽章、启用复选框、按 provider 类型的 cookie 输入框
  （yuanbao 用 hy_token/hy_user 双输入，qwen/kimi 用单输入）、
  agentId（yuanbao 显示，其它灰）、maxConcurrency / queueTimeoutSeconds /
  requestCooldownMs 三个数字输入、"保存此站点"按钮

#### Scenario: 切换默认 provider

- **WHEN** 运维在"默认 provider"下拉框选 `qwen` 并保存
- **THEN** 面板 POST
  `{ defaultProvider: "qwen" }` 到 `/api/config`，
  后续 `/v1/models` 的顶层 `ownedBy` 缺省的 model 走 qwen

#### Scenario: 启用一个 provider

- **WHEN** 运维把 kimi 的"启用"复选框勾上并保存
- **THEN** 面板 POST
  `{ providers: { kimi: { enabled: true } } }` 到 `/api/config`
- **AND** `GET /v1/models` 现在包含 kimi 的模型

#### Scenario: 保存 yuanbao cookie

- **WHEN** 运维在 yuanbao 的 hy_token 与 hy_user 输入框填值并点"保存此站点"
- **THEN** 面板 POST
  `{ providers: { yuanbao: { cookie: { hyToken, hyUser } } } }` 到 `/api/config`
- **AND** 响应 200 时显示"已保存。点击'重启服务'按钮生效。"

### Requirement: 兼容性

`runtime_config.json` 旧顶层字段（`maxConcurrency` /
`queueTimeoutSeconds` / `requestCooldownMs` / `yuanbaoCookie`）SHALL
被自动迁移到 `providers.yuanbao` 形态；旧 `POST /api/config` 形态
（含 `yuanbaoCookie` 等）SHALL 同样被翻译到新形态。两次翻译都不
需要运维手改文件。

#### Scenario: 旧文件无手改加载

- **WHEN** `runtime_config.json` 含旧形态字段
- **THEN** `LoadRuntimeConfig()` 返回的 `RuntimeConfig`
  其 `Providers["yuanbao"]` 含 cookie / concurrency 等字段
- **AND** `DefaultProvider == "yuanbao"`

#### Scenario: 旧请求体被翻译

- **WHEN** `POST /api/config` 收到旧形态请求
- **THEN** handler 翻译为新形态后写入持久化
- **AND** 响应体反映新形态（含 `providers`、`defaultProvider`）
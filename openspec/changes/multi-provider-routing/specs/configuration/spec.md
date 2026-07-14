# Spec: configuration（增量）

> 本 change 修改 `configuration` 领域既有 Requirement（`RuntimeConfig`
> 结构、`HandleSetConfig` 形态、`/api/status` 输出 shape、限流器职责），
> 全部为 MODIFIED。MODIFIED 段必须包含既有全部 scenarios + 必要的修改，
> 否则归档时会丢失。

## MODIFIED Requirements

### Requirement: Cookie 持久化至 runtime_config.json

> 修改范围：存储结构从单层字段升级为 `providers` 站点表 + `defaultProvider`。
> 写入语义（原子性、`0600` 权限、Windows rename 行为）保持不变
> （已由 `fix-windows-rename` 锁定）。Cookie 字段双形态语义保持不变
> （已由 `yuanbao-cookie-fields` 锁定）。

系统 SHALL 把运行时配置以**多 provider 表**的形式持久化到
`runtime_config.json`，结构为：

```json
{
  "providers": {
    "yuanbao": { "enabled": true, "cookie": { "hyToken": "...", "hyUser": "..." }, "agentId": "...", "maxConcurrency": 1, "queueTimeoutSeconds": 120, "requestCooldownMs": 800 },
    "qwen":    { "enabled": false },
    "kimi":    { "enabled": false }
  },
  "defaultProvider": "yuanbao"
}
```

每个 provider 的配置 SHALL 包含：
- `enabled`（可选，缺省 false）：该 provider 是否接受请求。
- `cookie`（可选）：`YuanbaoCookie` 对象，qwen/kimi 占位只用 `hyToken`。
- `agentId`（可选）：yuanbao 专属；qwen/kimi 忽略。
- `maxConcurrency` / `queueTimeoutSeconds` / `requestCooldownMs`
  （三者皆可选）：该 provider 的限流参数，缺省回落至环境变量
  （`MAX_CONCURRENCY` / `QUEUE_TIMEOUT_SECONDS` / `REQUEST_COOLDOWN_MS`），
  再回落至默认 1/120/0。

文件 SHALL 以原子方式写入，权限 `0600`，与既有 `SaveRuntimeConfig`
语义保持一致。

为支持从 `runtime-cookie` 期间的旧文件格式迁移，
`RuntimeConfig` SHALL 自定义 `UnmarshalJSON` 同时接受：
1. **新形态** `{providers: {...}, defaultProvider: "..."}`；
2. **旧形态** `{yuanbaoCookie: "...", maxConcurrency: ...}` —— 旧顶层
   字段被归到 `providers.yuanbao`，`defaultProvider = "yuanbao"`。
   `YuanbaoCookie` 自身的字符串/对象双形态（由 `yuanbao-cookie-fields`
   锁定）继续生效。

序列化输出 SHALL 始终是新形态。

#### Scenario: 持久化往返

- **WHEN** 运维经 `POST /api/config { providers: { yuanbao: { enabled: true, cookie: { hyToken: "abc", hyUser: "xyz" } } }, defaultProvider: "yuanbao" }` 保存
- **THEN** 磁盘上 `runtime_config.json` 包含 `providers.yuanbao.cookie`
  为 `{"hyToken":"abc","hyUser":"xyz"}`，`defaultProvider` 为 `"yuanbao"`
- **AND** 重启后 `LoadRuntimeConfig()` 返回的 `RuntimeConfig`
  其 `Providers["yuanbao"].Cookie.HyToken == "abc"`、
  `Providers["yuanbao"].Cookie.HyUser == "xyz"`、
  `DefaultProvider == "yuanbao"`

#### Scenario: 向后兼容加载

- **WHEN** `runtime_config.json` 已存在但不含 `yuanbaoCookie` 字段
  （早期 `runtime-cookie` 之前的文件）
- **THEN** `LoadRuntimeConfig()` 返回的 `RuntimeConfig` 其
  `Providers` 为空（nil/空 map），`DefaultProvider == ""`
- **AND** Cookie 解析逻辑回落至 env 值

#### Scenario: 旧字符串形式自动迁移

- **WHEN** `runtime_config.json` 已存在且
  `yuanbaoCookie` 为字符串 `"hy_token=legacy; hy_user=old"`
  （`runtime-cookie` 期间可能落地的格式）
- **THEN** `LoadRuntimeConfig()` 返回的 `RuntimeConfig`
  其 `Providers["yuanbao"].Cookie.HyToken == "legacy"`、
  `Providers["yuanbao"].Cookie.HyUser == "old"`、
  `DefaultProvider == "yuanbao"`
- **AND** 旧顶层字段 `maxConcurrency` / `queueTimeoutSeconds` /
  `requestCooldownMs` 被归到 `Providers["yuanbao"]` 对应字段

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

> 修改范围：`EffectiveYuanbaoCookie()` 现在是 yuanbao provider 专属，
> 调用方从 `provider.Registry().Yuanbao().EffectiveCookie()` 获取，
> 但**外部行为完全保持不变**。

系统 SHALL 在每次发起上游请求时（而非在客户端构造时）解析 yuanbao
provider 使用的 Cookie，按以下优先级：

1. `runtime_config.json:providers.yuanbao.cookie`（已设且 `HyToken` 或
   `HyUser` 任一非空时）
2. `YUANBAO_COOKIE` 环境变量（已设且非空时）—— 作为整段 Cookie 头
   字符串原样使用
3. 空字符串（调用方按"无 Cookie"处理）

解析逻辑 SHALL 封装在 yuanbao provider 内部（`providers/yuanbao`
包），所有上游调用方通过 yuanbao provider 实例的 `EffectiveCookie()`
方法读取，禁止直接读 env。

当优先级 1 命中时，组装 SHALL 按 `hy_token=<HyToken>; hy_user=<HyUser>`
格式进行；任一字段为空时对应段 SHALL 省略。

#### Scenario: 运行时双字段覆盖 env

- **GIVEN** `runtime_config.json` 中
  `providers.yuanbao.cookie = {"hyToken":"runtime-tok","hyUser":"runtime-usr"}`
  **AND** env `YUANBAO_COOKIE` 为 `"env-cookie"`
- **WHEN** yuanbao provider 发送请求
- **THEN** `Cookie` 请求头为 `"hy_token=runtime-tok; hy_user=runtime-usr"`

#### Scenario: env 兜底

- **GIVEN** `runtime_config.json` 中 yuanbao provider 的 `cookie` 字段未设
  **AND** env `YUANBAO_COOKIE` 为 `"env-cookie"`
- **WHEN** yuanbao provider 发送请求
- **THEN** `Cookie` 请求头为 `"env-cookie"`

#### Scenario: 两者皆空

- **GIVEN** yuanbao provider 无 cookie **AND** env 无 Cookie
- **WHEN** yuanbao provider 发送请求
- **THEN** 出站请求不设置 `Cookie` 请求头
- **AND** 上游按预期返回 4xx，由 API 层以普通上游错误形式回传调用方

### Requirement: 经管理 API 更新 Cookie

> 修改范围：请求体形态从扁平字段（`yuanbaoCookie`）升级为嵌套
> `providers` 对象。**双形态兼容**：`HandleSetConfig` 同时接受
> 旧 `{yuanbaoCookie, maxConcurrency, ...}` 形态与新
> `{providers: {...}, defaultProvider: "..."}` 形态；旧形态被
> 翻译到新形态后写入持久化。

`POST /api/config` SHALL 同时支持两种请求体形态：

**新形态** `{providers: {<name>: ProviderConfig}, defaultProvider: "..."}`：
- `providers` 必须为 object；每条 provider 配置按既有规则校验。
- `defaultProvider` 可选；缺省则保持当前值不变。

**旧形态**（向后兼容，触发条件：请求体中**没有** `providers` 字段
但**有** `yuanbaoCookie` / `maxConcurrency` / `queueTimeoutSeconds`
/ `requestCooldownMs` 中任一旧字段）：
- 旧字段被映射到 `providers.yuanbao`；`defaultProvider = "yuanbao"`。
- 与新形态的 Cookie 校验规则一致。

端点 SHALL 继续沿用现有的部分更新规则：缺省字段 MUST NOT 清零既有值
（与既有 `deepThinking` 等字段一致）。

#### Scenario: 保存双字段 Cookie（新形态）

- **WHEN** `POST /api/config` 收到
  `{ "providers": { "yuanbao": { "enabled": true, "cookie": { "hyToken": "abc", "hyUser": "xyz" } } }, "defaultProvider": "yuanbao" }`
- **THEN** 响应为 HTTP 200，并附最新服务端配置
- **AND** `runtime_config.json` 包含
  `providers.yuanbao.cookie = {"hyToken":"abc","hyUser":"xyz"}`、
  `defaultProvider = "yuanbao"`

#### Scenario: 旧形态请求被自动迁移

- **WHEN** `POST /api/config` 收到
  `{ "yuanbaoCookie": { "hyToken": "abc", "hyUser": "xyz" } }`（**没有** `providers` 字段）
- **THEN** 响应为 HTTP 200，并把请求体翻译为
  `{ providers: { yuanbao: { enabled: true, cookie: {...} } }, defaultProvider: "yuanbao" }` 后持久化
- **AND** `runtime_config.json` 反映新形态

#### Scenario: 用全空对象清除运行时 Cookie

- **WHEN** `POST /api/config` 收到
  `{ "providers": { "yuanbao": { "cookie": {} } } }` 且当前
  已持久化有运行时 Cookie
- **THEN** 响应为 HTTP 200
- **AND** 下次 `LoadRuntimeConfig()` 返回
  `Providers["yuanbao"].Cookie == nil`
- **AND** 后续上游请求从 env 解析 Cookie

#### Scenario: 缺省字段为 no-op

- **WHEN** `POST /api/config` 收到 `{ "deepThinking": true }`，无
  `providers` 与 `yuanbaoCookie` 字段
- **THEN** 既有运行时配置（所有 provider）保持不变
- **AND** 不会以清空方式重写 `runtime_config.json`

#### Scenario: 类型错误

- **WHEN** `POST /api/config` 收到
  `{ "providers": { "yuanbao": { "cookie": "not-an-object" } } }`
- **THEN** 响应为 HTTP 400，并附说明性错误信息

### Requirement: 经 /api/env 暴露 Cookie 来源

> 修改范围：响应体增加 `providers` 摘要与 `defaultProvider` 字段；
> 旧顶层 Cookie 字段保留以维持旧客户端。

`GET /api/env` SHALL 在原有字段之外额外包含：
- `defaultProvider`（string）：当前默认 provider 名字。
- `providers`（object）：每个 provider 的关键字段摘要（name、enabled、
  cookieSource、cookie 掩码、agentId 掩码、concurrency）。
- 旧 `yuanbaoCookie` / `yuanbaoHyToken` / `yuanbaoHyUser` 字段保留，
  取自 `defaultProvider`（默认 yuanbao）。
- 旧 `cookieSource` 字段保留。

#### Scenario: 显示来源

- **WHEN** runtime `Providers["yuanbao"].Cookie` 为
  `{"hyToken":"abcdef1234","hyUser":"uvwxyz5678"}`，env Cookie 未设
- **THEN** `GET /api/env` 返回
  ```json
  {
    "defaultProvider": "yuanbao",
    "providers": {
      "yuanbao": {
        "enabled": true,
        "cookieSource": "runtime",
        "yuanbaoCookie": "hy_toke****",
        "yuanbaoHyToken": "abcdef1****",
        "yuanbaoHyUser": "uvwxyz****"
      }
    },
    "cookieSource": "runtime"
  }
  ```

#### Scenario: env 兜底报告 env 来源

- **WHEN** runtime yuanbao cookie 未设，env `YUANBAO_COOKIE` 已设
- **THEN** `GET /api/env` 返回 `cookieSource: "env"`

### Requirement: 面板 Cookie 编辑入口

> 修改范围：UI 升级。新增"站点管理"tab 作为推荐入口；旧"配置"tab
> 的元宝 Cookie 字段**保留**但仅作旧入口，新工作流推荐"站点管理"。

管理面板 SHALL 提供**两个独立输入框**用于 yuanbao provider 的
Cookie 编辑（沿用 `yuanbao-cookie-fields` 锁定）：
- `id="yuanbaoHyTokenInput"`
- `id="yuanbaoHyUserInput"`

输入框 SHALL 默认隐藏（带显隐切换）。每个输入框 SHALL 在
`GET /api/env` 返回时被预填充为对应字段的掩码值。

保存 SHALL POST 到 `/api/config`：
- 新形态优先：
  `{ providers: { yuanbao: { cookie: { hyToken, hyUser } } } }`
- 旧形态回退（仅旧"配置"tab 仍可走）：
  `{ yuanbaoCookie: { hyToken, hyUser } }`

#### Scenario: 运维经面板更新 Cookie

- **GIVEN** 运维已在两个输入框分别填好新值
- **WHEN** 运维点击"保存 Cookie"
- **THEN** 面板向 `/api/config` 发起
  `{ providers: { yuanbao: { cookie: { hyToken: <tok>, hyUser: <usr> } } } }` 的 POST
- **AND** HTTP 200 时，面板显示"已保存。点击'重启服务'按钮生效。"
- **AND** HTTP 非 200 时，面板显示 API 返回的错误信息

### Requirement: 原子 rename 助手 internal 契约

> 既有 Requirement（`fix-windows-rename` 引入），行为不变。

`atomicRename(tmp, target string) error` SHALL 在 Windows 上
"重试 → 必要时 fallback 到 remove+rename" 的语义保持不变。

#### Scenario: 原子 rename 成功

- **WHEN** target 不存在或可被 rename 覆盖
- **THEN** `atomicRename` 返回 nil
- **AND** target 文件存在且内容等于 tmp 内容

#### Scenario: 原子 rename 在 Windows 上重试

- **GIVEN** 首次 `os.Rename` 返回 `Access is denied` 错误
- **WHEN** 调用 `atomicRename`
- **THEN** helper 在短暂重试或 fallback 后最终成功

#### Scenario: 原子 rename 错误传播

- **GIVEN** tmp 不存在
- **WHEN** 调用 `atomicRename`
- **THEN** 返回非 nil 错误
- **AND** target 文件不被修改

### Requirement: 运行时限流

> 修改范围：限流器职责从"全局单例"升级为"按 provider 持有"。
> 每个 provider 独立并发额度（来自 `Providers[name].MaxConcurrency`
> 或环境变量回退）。

系统 SHALL 通过 `LimiterManager` 管理每个 provider 的并发额度。
每个 provider 一次至多 `MaxConcurrency` 个请求同时出站；超额请求
按 FIFO 排队直到有 slot 释放或 `QueueTimeoutSeconds` 超时。
`Release()` 在释放前 sleep `RequestCooldownMs` 毫秒。

`LimiterManager.For(providerName string) *RateLimiter` SHALL：
- 第一次调用时按 `Providers[providerName]` 的字段（或环境变量回退，
  或默认值 1/120/0）构造 `RateLimiter` 并缓存。
- 后续调用返回缓存实例。
- 未知 provider 名 → 构造一个 "pass-through" limiter（maxConcurrency
  足够大），避免 panic —— 调用方预期在 `Route` 阶段就拒绝未知 provider。

#### Scenario: 每个 provider 独立并发

- **GIVEN** yuanbao provider `MaxConcurrency = 1`、
  qwen provider `MaxConcurrency = 3`
- **WHEN** 同时有 1 个 yuanbao 请求 + 3 个 qwen 请求在飞行
- **THEN** 第 2 个 yuanbao 请求进入排队，第 4 个 qwen 请求进入排队

#### Scenario: queue timeout 触发

- **GIVEN** provider `MaxConcurrency = 1`、`QueueTimeoutSeconds = 5`
- **WHEN** 一个请求已占 slot 持续 6 秒，另一个请求在等 slot
- **THEN** 等待的请求收到 HTTP 429 "queue timeout"

#### Scenario: Release 触发 cooldown

- **GIVEN** provider `RequestCooldownMs = 500`
- **WHEN** 一个请求完成
- **THEN** 该 slot 在 500ms 后才被标记为空

#### Scenario: 未知 provider 走 pass-through

- **WHEN** 调用 `LimiterManager.For("nonexistent")`
- **THEN** 返回一个 maxConcurrency 极大的 limiter（不阻塞）

### Requirement: /api/status 形状

> 修改范围：响应从单层 stats 升级为按 provider 切片；顶层字段保留
> 以维持仪表盘卡片。

`GET /api/status` SHALL 返回：

```json
{
  "providers": {
    "yuanbao": { "maxConcurrency": 1, "queueTimeoutSeconds": 120, "requestCooldownMs": 800, "inflight": 0, "waiting": 0 },
    "qwen":    { "maxConcurrency": 0, "queueTimeoutSeconds": 0, "requestCooldownMs": 0, "inflight": 0, "waiting": 0 }
  },
  "maxConcurrency": 1, "queueTimeoutSeconds": 120, "requestCooldownMs": 800, "inflight": 0, "waiting": 0
}
```

顶层 `maxConcurrency` / `inflight` / `waiting` / `requestCooldownMs`
字段 SHALL 取自 `defaultProvider` 的值（默认 yuanbao），维持既有
仪表盘卡片无回归。

#### Scenario: 多 provider 状态可见

- **WHEN** registry 包含 yuanbao（启用，inflight=1, waiting=2）和
  qwen（未启用，inflight=0, waiting=0）
- **THEN** `GET /api/status` 返回两个 provider 各自的 stats，
  顶层 stats 取自 defaultProvider（yuanbao）

### Requirement: /v1/models 模型并集

> 修改范围：`/v1/models` 从只列 yuanbao 模型升级为所有启用 provider
> 的模型并集。

`GET /v1/models` SHALL 返回 `Registry.All()` 中**已启用** provider 的
`Models()` 拼接结果。每个 `ModelInfo` 的 `ownedBy` 字段 SHALL 为
对应 provider 名字。

未启用 provider 的模型 SHALL 不出现在响应中。

qwen/kimi 占位 provider 在未启用时不出现在响应中（其 Models() 仍
返回官方模型名，但被 `enabled == false` 过滤掉）。

#### Scenario: 多 provider 模型并集

- **GIVEN** registry 含 yuanbao（启用，2 个模型）与 qwen（启用，4 个模型）
- **WHEN** 调用 `GET /v1/models`
- **THEN** 响应包含 6 个 ModelInfo，yuanbao 那 2 个 `ownedBy="yuanbao"`、
  qwen 那 4 个 `ownedBy="qwen"`

#### Scenario: 停用的 provider 模型被过滤

- **GIVEN** kimi 未启用
- **WHEN** 调用 `GET /v1/models`
- **THEN** 响应不含 kimi 的模型

# Spec: provider-registry（新领域）

> 本 change 新建 `provider-registry` 领域，记录 `Provider` 接口、
> `Registry`、`StreamChunk` 等抽象的行为契约。

## ADDED Requirements

### Requirement: Provider 接口

`Provider` 接口 SHALL 描述任何上游 web2api 必须实现的方法集。

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
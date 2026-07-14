# 提案：multi-provider-routing

## Why

`yuanbao2api` 目前只支持元宝（Tencent Yuanbao）一家上游。Cookie 入运行时
配置（`runtime-cookie`）让元宝的运维体验改善了，但整个架构仍然是
"元宝专用"：

- `api/openai.go` 与 `api/anthropic.go` 直接 `import "yuanbao2api/yuanbao"`，
  直接 `yuanbao.NewClient().SendRequestWithID(...)`。
- `api/models.go` 的 `MODEL_MAPPING` 把模型名硬映射到元宝的 `chatModelId`。
- `api/ratelimit.go` 只有一个全局 `globalRateLimiter` 信号量，
  所有出站请求共用一个并发额度。
- `yuanbao/client.go` 里的硬编码头（`x-source: web`、UA 字符串、
  `x-webversion: 2.63.0`）和元宝专属请求体结构（`YuanbaoRequest`）
  与 `api/openai.go` 的 prompt 构造深度耦合。

用户希望按相同模式接入更多 web2api（如千问、Moonshot Kimi）。
约束：每家**只一个账号**、**不轮询**；每家独立配置（Cookie、agentID、
并发额度）；所有模型**统一**进 `POST /v1/chat/completions` 与
`POST /v1/messages`，由请求体的 `model` 名字段路由到对应 provider。

## What Changes

### 1. Provider 抽象

新增 `provider` 包 + `Provider` 接口。每个 provider（如 yuanbao、qwen、
kimi）实现该接口，注册到 `provider.Registry`。

```go
package provider

// Provider 上游 web2api 的统一抽象。
type Provider interface {
    // Name 返回 provider 标识，用于 registry 查找与 /api/config 字段。
    Name() string

    // Models 返回 provider 官方支持的全部模型名（含对外展示名）。
    Models() []ModelInfo

    // BuildPrompt 把 OpenAI/Anthropic 风格的 messages + tools 拼成
    // provider 内部的 prompt 字符串（与 tool system prompt）。
    // 元宝类纯 prompt 调用的网站直接拼接；将来若有原生 tool calling，
    // provider 自行决定是否使用原生协议。
    BuildPrompt(messages []Message, tools []Tool) (prompt, toolSystem string, err error)

    // NewRequest 根据 prompt + 选项构造 provider 特定的请求体。
    NewRequest(prompt string, opts RequestOptions) (any, error)

    // Send 发送请求并返回原始 *http.Response。
    // 失败语义：连接错误、超时、上游 4xx/5xx 都通过 (nil, err) 表达。
    Send(req any, agentID, conversationID string) (*http.Response, error)

    // ParseStreamLine 把 SSE 一行解析为统一的 StreamChunk。
    ParseStreamLine(line string) (*StreamChunk, error)
}

// StreamChunk 统一所有 provider 的流式响应切片。
type StreamChunk struct {
    Type     string // "think" | "text" | ...
    Content  string // reasoning / thinking 内容（Type=="think" 时）
    Text     string // 正文（Type=="text" 时）
}

// ModelInfo / Message / Tool / RequestOptions 在 provider 包定义。
```

**关键设计**：每个 provider 自行负责 `BuildPrompt` / `ParseStreamLine` 的
所有差异，调用方只通过 `StreamChunk` 三个字段消费。

### 2. 目录结构

新增 `providers/` 目录，yuanbao 包迁入：

```
providers/
├── provider.go            # Provider 接口、Registry、StreamChunk、辅助类型
├── registry.go            # 全局 Registry，按模型名 + provider 名路由
├── yuanbao/
│   ├── client.go          # 从 yuanbao/client.go 原样迁入
│   ├── provider.go        # yuanbao.Provider 实现
│   ├── provider_test.go
│   └── prompt.go          # 从 api/* 抽出 prompt 构造逻辑
├── qwen/
│   ├── client.go          # 占位：NewClient 返回最小 client
│   ├── provider.go        # 占位：Name/Models 正常；其余返回 "not implemented"
│   └── provider_test.go
└── kimi/
    ├── client.go          # 同上
    ├── provider.go        # 同上
    └── provider_test.go
```

原 `yuanbao/` 目录在所有引用迁移完后**删除**。

### 3. `RuntimeConfig` 升级为多站点表

```go
type RuntimeConfig struct {
    Providers       map[string]ProviderConfig `json:"providers,omitempty"`
    DefaultProvider string                    `json:"defaultProvider,omitempty"`
}

type ProviderConfig struct {
    Enabled             *bool        `json:"enabled,omitempty"`
    Cookie              *YuanbaoCookie `json:"cookie,omitempty"`
    AgentID             *string      `json:"agentId,omitempty"`
    MaxConcurrency      *int         `json:"maxConcurrency,omitempty"`
    QueueTimeoutSeconds *int         `json:"queueTimeoutSeconds,omitempty"`
    RequestCooldownMs   *int         `json:"requestCooldownMs,omitempty"`
}
```

`Cookie` 暂统一为 `*YuanbaoCookie`（双字段结构）。qwen/kimi 占位
只用 `HyToken` 字段保存单一 cookie 字符串，`HyUser` 留空。将来若
qwen/kimi 真实接入了，再分别定义 cookie 形状。

### 4. `RuntimeConfig` 向后兼容

旧的 `runtime_config.json`（`runtime-cookie` 期间格式）结构为：
```json
{
  "maxConcurrency": 1,
  "queueTimeoutSeconds": 120,
  "requestCooldownMs": 800,
  "yuanbaoCookie": { "hyToken": "...", "hyUser": "..." }
}
```

`RuntimeConfig` 实现自定义 `UnmarshalJSON`：先尝试新形态（`providers`
+ `defaultProvider`），失败则按旧形态解析（顶层字段归到 `providers.yuanbao`，
`defaultProvider = "yuanbao"`）。序列化输出新形态。

旧形态到新形态的字段映射：
- `maxConcurrency` / `queueTimeoutSeconds` / `requestCooldownMs` →
  `providers.yuanbao.{同名}`
- `yuanbaoCookie` → `providers.yuanbao.cookie`
- `defaultProvider` = `"yuanbao"`

### 5. 限流器：全局单例 → 按 provider 持有

`api/ratelimit.go` 的 `globalRateLimiter` 替换为 `LimiterManager`：

```go
type LimiterManager struct {
    mu       sync.RWMutex
    limiters map[string]*RateLimiter // provider name -> limiter
}

func (m *LimiterManager) For(providerName string) *RateLimiter
```

handler 在 `provider.Registry().Route(model).Name()` 之后调用
`limiterManager.For(providerName).Acquire(...)` / `Release()`。

每个 provider 的 `MaxConcurrency` / `QueueTimeoutSeconds` /
`RequestCooldownMs` 由 `RuntimeConfig.Providers[providerName]` 提供，
回退到 `getEnvInt`（`MAX_CONCURRENCY_<NAME>` 等环境变量），再回退
到 `MAX_CONCURRENCY` 全局变量，最后回退到默认值 1/120/0。

`/api/status` 输出 shape 升级为：
```json
{
  "providers": {
    "yuanbao": { "maxConcurrency": 1, "inflight": 0, "waiting": 0, "requestCooldownMs": 800 },
    "qwen":    { "maxConcurrency": 0, "inflight": 0, "waiting": 0, "requestCooldownMs": 0 }
  }
}
```
保留 `maxConcurrency` / `inflight` / `waiting` / `requestCooldownMs` 顶层
字段以维持仪表盘卡片，向后兼容（取自 `defaultProvider`）。

### 6. 模型路由

`api/models.go` 的 `MODEL_MAPPING` + `GetModelConfig()` 替换为
`provider.Registry().Route(modelName string) (Provider, error)`：

- 显式 `modelName` 在 `defaultProvider` 的 model 列表中 → 走它
- 否则尝试其它已启用 provider 的 model 列表
- 都不命中 → 400 "unknown model"
- 命中但 provider 未启用 → 503 "provider disabled"
- 命中但 provider 是占位（qwen/kimi）→ 501 "not implemented"

### 7. `/v1/models` 输出 provider 模型并集

遍历 `Registry.All()`，把每个启用 provider 的 `Models()` 拼成
`/v1/models` 响应。每个 ModelInfo 增加 `ownedBy` = provider 名字段
（已有），保持结构稳定。

### 8. qwen / kimi 占位

`providers/qwen/provider.go`（结构同 kimi）：
- `Name() = "qwen"`
- `Models()` 返回千问官方主推模型名（如 `qwen-max`、`qwen-plus`、
  `qwen-turbo`、`qwen-long`），每个 ModelInfo 的 `ownedBy = "qwen"`
- `BuildPrompt` / `NewRequest` / `Send` 全部返回错误
  `"qwen provider is not yet implemented"`
- `ParseStreamLine` 始终返回 `nil, nil`（即没解析到任何内容）
- 单元测试覆盖 `Name` / `Models` / 各方法的错误返回

kimi 同理（`kimi-k2`、`moonshot-v1-128k` 等模型名）。

### 9. 面板：新增"站点管理" tab

在 `public/index.html` 现有 4 个 tab（dashboard / testing / config /
info）后追加第 5 个 tab **"站点管理"**。其内容：

- 顶部："默认 provider" 下拉框（来自 `Registry.All()` 中已启用的）
- 每个 provider 一节（折叠面板）：
  - **启用 / 停用** 复选框
  - **状态徽章**："已配置 / 未配置 / 停用 / 占位（未实现）"
  - **yuanbao**：hy_token / hy_user 输入框（沿用 `yuanbao-cookie-fields`）
  - **qwen / kimi**：单一 Cookie 输入框
  - **agentID**（yuanbao 显示，其它灰）
  - **maxConcurrency / queueTimeoutSeconds / requestCooldownMs** 三个数字输入
  - "保存此站点" 按钮
- 现有"配置"tab 中关于元宝 Cookie 的字段**保留**作为旧入口，
  但新工作流推荐使用"站点管理"。

### 10. 兼容性摘要

- **磁盘 `runtime_config.json`**：自定义 `UnmarshalJSON` 同时识别旧
  顶层字段与新 `providers` 形态；首次保存后写入新形态。旧文件
  无手改可加载。
- **`yuanbao/` 顶层包**：删除，调用方已迁移到 `providers/yuanbao/`。
- **`api.EffectiveYuanbaoCookie()`**：保留为 `provider.YuanbaoCookie()`
  的薄包装（或迁移到 `providers/yuanbao` 包内），
  `yuanbao.CookieResolver` 委托关系不变。
- **`runtime-cookie` / `yuanbao-cookie-fields` 既有行为**：完全保持；
  Cookie 双字段、拼装、解析兼容性等无回归。
- **`POST /api/config` 请求体**：从 `{ yuanbaoCookie: {...} }` 升级为
  `{ providers: { yuanbao: { cookie: {...}, enabled: true, ... } } }`。
  **新旧请求体形态不同时被支持**：`HandleSetConfig` 先尝试新形态
  （顶层有 `providers` 键），否则走旧字段路径（`yuanbaoCookie` 等），
  写入新形态持久化。

## Impact

- 受影响 spec：
  - `configuration`（MODIFIED — `RuntimeConfig` 结构、限流器、API handler 升级）
  - 新增 `provider-registry` 领域
- 受影响文件（约 12–14 个）：
  - `api/openai.go`、`api/anthropic.go`（handler 改走 Registry）
  - `api/config.go`、`api/config_persist.go`、`api/env.go`（config 升级）
  - `api/ratelimit.go`（manager 化）
  - `api/models.go`（替换为 Registry 路由）
  - `main.go`（初始化 Registry、LimiterManager）
  - `providers/provider.go`、`providers/registry.go`（新）
  - `providers/yuanbao/{client,provider,prompt}.go`（从 yuanbao/ 与 api/ 迁入）
  - `providers/qwen/{client,provider}.go`（新，占位）
  - `providers/kimi/{client,provider}.go`（新，占位）
  - `public/index.html`、`public/app.js`（站点管理 tab + 旧配置 tab 适配）
  - 各 `*_test.go`（覆盖新行为）
- 删除：`yuanbao/` 顶层目录（迁移完成后）。
- 不引入新依赖。
- 不修改 `session/`、`toolcall/`、`internal/`（与 provider 抽象无关，
  维持纯工具函数）。

## Compatibility

- **旧 `runtime_config.json` 文件**：通过 `UnmarshalJSON` 自动迁移到
  新 `providers` 形态；旧文件无手改可加载，**首次 POST /api/config
  后会写为新形态**。
- **旧 `POST /api/config` 请求体**（`{ yuanbaoCookie: {...} }`）：
  通过 `HandleSetConfig` 双形态支持；**首次成功保存后，响应体的
  `yuanbaoCookie` 字段会归零（被搬到 `providers.yuanbao.cookie`）**。
  旧客户端从此需读 `providers.yuanbao.cookie`。
- **`/api/status` 顶层字段**：保留（取自 defaultProvider），旧仪表盘
  卡片无回归。
- **`/v1/models`**：增加 `ownedBy` 字段语义（已存在），新 provider 模型
  出现在列表里。
- **`yuanbao/` 顶层包导入**：删除。如果有外部代码 import 该路径，
  编译失败 —— 仓库内部已无外部消费者。

## Approach（高层）

按以下顺序实施，每一步独立可测：

1. **新增 `providers/provider.go` 与 `providers/registry.go`** —— 定义
   `Provider` 接口、`StreamChunk`、`Registry`、`ModelInfo` 等。
2. **迁 `yuanbao/client.go` → `providers/yuanbao/client.go`**，
   并新增 `providers/yuanbao/provider.go` 实现 `Provider` 接口。
3. **抽 `api/openai.go` 与 `api/anthropic.go` 中的 prompt 构造**到
   `providers/yuanbao/prompt.go`（包内方法），handler 不再直接 import
   yuanbao 类型。
4. **新增 `providers/qwen/` 与 `providers/kimi/` 占位**。
5. **`api/ratelimit.go` 改造**：从单例到 `LimiterManager`；handler
   按 `provider.Name()` 取 limiter。
6. **`api/config_persist.go` 与 `api/config.go` 升级**：
   - `RuntimeConfig` 新结构 + 自定义 `UnmarshalJSON` 双形态
   - `ServerConfigData` 增加 `DefaultProvider` 字段
   - `HandleSetConfig` 接受新 `{providers: {...}}` 形态，保留旧
     `{yuanbaoCookie: ...}` 形态回退
7. **`api/env.go` 升级**：响应包含 `defaultProvider` + `providers` 摘要。
8. **`api/models.go` 替换**：`GetModelConfig` 改走 `Registry.Route`；
   `MODEL_MAPPING` 移到 `providers/yuanbao` 包内（不变更对外逻辑）。
9. **`/v1/models` 升级**：遍历 `Registry.All()` 输出。
10. **面板新增"站点管理" tab + 旧 config tab 字段重定向到
    `providers.yuanbao.{cookie,enabled,concurrency}`**。
11. **删除 `yuanbao/` 顶层目录**。
12. **测试**：覆盖 Registry 路由、双形态 JSON 兼容、LimiterManager
    按 provider 取限流器、各 provider 的 Name/Models、qwen/kimi
    占位错误返回、面板站点管理 tab 的 DOM 节点（DOM-level 单元
    测试可选 —— 当前项目无前端测试套件，可手测替代）。
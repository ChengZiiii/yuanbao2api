# 设计：multi-provider-routing

## 架构决策

### 1. `Provider` 接口形态选择

考察三种方案：

**A. `any` 强类型 + 类型断言**。`NewRequest` 返回 `any`，handler 在
调用 `Send` 时做断言。优点是简单；缺点是失去编译期保护，每个 provider
改动都要 handler 跟着改。

**B. 泛型 `Provider[TReq, TChunk]`**。编译期保证类型一致；缺点是
Registry 难以持有异构泛型实例，需要类型擦除或 `interface{}` 列表，
最后还是回到 any。

**C. 接口方法使用 `any` + 各 provider 自行做类型断言**（采用）。
在 `Provider` 接口层面，方法参数/返回用 `any`；调用方对返回值用
类型断言或交给 provider 自己的 `Send(req, ...)` 转发。Yuanbao
具体类型 `YuanbaoRequest` 仍保留在 `providers/yuanbao` 包内，
不外泄到 `provider` 包或其它 provider。Handler 不直接断言 —
它把 `BuildPrompt` 的产物和 `NewRequest` 的产物交给 `Send`，
`Send` 内部断言（每个 provider 的 `Send` 知道自己的请求类型）。

**理由**：C 是 Go 接口设计的常态，handler 不需要知道 provider 内部
类型，编译期保护由"每个 provider 自己完整实现"保证。新增 provider
时只需要新加一个文件，不动其它文件。

### 2. `StreamChunk` 简化

`YuanbaoResponseChunk` 当前有三个字段 `Type / Content / Msg`。
统一为 `StreamChunk{Type, Content, Text}`：
- `Type == "think"` 时 `Content` 是思考内容。
- `Type == "text"` 时 `Text` 是正文。
- 其它 `Type` 走忽略路径。

Yuanbao 适配：`ParseStreamLine` 把 `Msg` 映射到 `Text`、`Content` 映射到 `Content`。

### 3. 限流器架构

`LimiterManager` 是一个**进程级**单例（在 `api/ratelimit.go` 改为
包级变量 `limiterManager`），按 provider name 懒构造 `RateLimiter`。
构造时优先级：
1. `Providers[providerName].{MaxConcurrency, QueueTimeoutSeconds, RequestCooldownMs}`
2. 环境变量 `MAX_CONCURRENCY` / `QUEUE_TIMEOUT_SECONDS` / `REQUEST_COOLDOWN_MS`
   （保持旧行为；不为每个 provider 提供独立 env 以减少表面）
3. 内置默认 1 / 120s / 0ms

`LimiterManager.For(name)` 对未知 name 返回 pass-through limiter
（maxConcurrency = 1<<30，cooldown = 0），不阻塞；调用方预期
在 `Registry.Route` 阶段就拒绝未知 model。

### 4. `RuntimeConfig.UnmarshalJSON` 双形态

```go
func (rc *RuntimeConfig) UnmarshalJSON(data []byte) error {
    // 1) 先尝试新形态
    type alias RuntimeConfig
    var s alias
    if err := json.Unmarshal(data, &s); err == nil && s.Providers != nil {
        *rc = RuntimeConfig(s)
        return nil
    }
    // 2) 回退到旧形态
    var legacy struct {
        MaxConcurrency      *int           `json:"maxConcurrency,omitempty"`
        QueueTimeoutSeconds *int           `json:"queueTimeoutSeconds,omitempty"`
        RequestCooldownMs   *int           `json:"requestCooldownMs,omitempty"`
        YuanbaoCookie       *YuanbaoCookie `json:"yuanbaoCookie,omitempty"`
    }
    if err := json.Unmarshal(data, &legacy); err != nil {
        return err
    }
    rc.Providers = map[string]ProviderConfig{
        "yuanbao": {
            Enabled:             ptr(true),
            Cookie:              legacy.YuanbaoCookie,
            MaxConcurrency:      legacy.MaxConcurrency,
            QueueTimeoutSeconds: legacy.QueueTimeoutSeconds,
            RequestCooldownMs:   legacy.RequestCooldownMs,
        },
    }
    rc.DefaultProvider = "yuanbao"
    return nil
}
```

序列化输出始终是 `json.Marshal(rc)` —— 不自定义 `MarshalJSON`，
Go 默认按 struct 字段顺序输出。

### 5. `HandleSetConfig` 双形态

`HandleSetConfig` 先检查 `body` 中是否有 `providers` 键：
- 有 → 新形态：按 `providers.<name>.<field>` 解析；
- 无 → 旧形态：检查 `body["yuanbaoCookie"]` / `body["maxConcurrency"]`
  / `body["queueTimeoutSeconds"]` / `body["requestCooldownMs"]`
  / `body["agentId"]` 中任一存在 → 翻译到 `Providers["yuanbao"]`；
- 都没有 → 旧"扁平字段"（`deepThinking` / `internetSearch` /
  `defaultModel` / `defaultProvider`）继续走原逻辑。

旧形态翻译时，`Providers["yuanbao"]` 的 `Enabled` 默认设 true（旧的
Yuanbao 配置存在即启用）。

### 6. `api/openai.go` 与 `api/anthropic.go` 改造

handler 改造的核心循环：
```go
prov, err := provider.Registry().Route(model)
if err != nil { /* 4xx/5xx with err */ }
rl := limiterManager.For(prov.Name())
_ = rl.Acquire(ctx)
defer rl.Release()

prompt, toolSystem, err := prov.BuildPrompt(messages, tools)
req, err := prov.NewRequest(prompt, opts)
resp, err := prov.Send(req, agentID, conversationID)
// 后续 handleOpenAIStream(c, resp, prov.ParseStreamLine, ...)
```

`handleOpenAIStream` 与 `handleAnthropicStream` 当前的 `bufio.Scanner`
循环里硬编码 `yuanbao.ParseStreamLine(line)`，改为接受一个
`func(line string) (*provider.StreamChunk, error)` 参数。UTF-8 安全的
自定义 `SplitFunc` 保持不变（与 provider 无关）。

### 7. `yuanbao/` 顶层包迁移

`yuanbao/client.go` 整体迁到 `providers/yuanbao/client.go`：
- `Config` / `Client` / `NewClient` / `SendRequestWithID` / `ParseStreamLine`
  全部保留。
- `CookieResolver` 函数变量保留（同包 init 时由 `api/config.go`
  通过 `init()` 注入 `EffectiveYuanbaoCookie` —— 但 `EffectiveYuanbaoCookie`
  也在迁移后从 `api` 包迁到 `providers/yuanbao` 包内，
  wiring 改为 `providers/yuanbao.init()` 内 `yuanbao.CookieResolver = yuanbao.EffectiveYuanbaoCookie`）。
  这样 `api` 包不再依赖 `EffectiveYuanbaoCookie` 的具体实现位置，
  也不需要 `CookieResolver` 的循环委托。

`yuanbao.YuanbaoRequest` 类型保留（现为 `YuanbaoRequest` 的 type alias）
用于内部请求体；handler 不再 import。

`api/models.go` 的 `MODEL_MAPPING` / `GetModelConfig` /
`buildYuanbaoRequest` 全部移到 `providers/yuanbao/provider.go` 内的
私有/包内方法。

### 8. `provider/registry.go` 设计

```go
package provider

type Registry struct {
    mu        sync.RWMutex
    providers []Provider
    defaultName string
}

var global = NewRegistry()

func Registry() *Registry { return global }

func (r *Registry) Register(p Provider) error
func (r *Registry) All() []Provider
func (r *Registry) Names() []string
func (r *Registry) Get(name string) (Provider, bool)
func (r *Registry) Default() Provider
func (r *Registry) SetDefault(name string) error
func (r *Registry) Route(modelName string) (Provider, error)
```

`main.go` 启动时 `Register(yuanbao.New())`、`Register(qwen.New())`、
`Register(kimi.New())`、`SetDefault("yuanbao")`。

### 9. qwen / kimi 占位

每个占位 provider 文件结构相同（60-80 行）：
```go
package qwen

type Provider struct{}

func New() *Provider { return &Provider{} }

func (p *Provider) Name() string { return "qwen" }
func (p *Provider) Models() []provider.ModelInfo { return []provider.ModelInfo{...} }
func (p *Provider) BuildPrompt(...) (string, string, error) {
    return "", "", errors.New("qwen provider is not yet implemented")
}
func (p *Provider) NewRequest(...) (any, error) {
    return nil, errors.New("qwen provider is not yet implemented")
}
func (p *Provider) Send(any, string, string) (*http.Response, error) {
    return nil, errors.New("qwen provider is not yet implemented")
}
func (p *Provider) ParseStreamLine(string) (*provider.StreamChunk, error) {
    return nil, nil
}
```

`Send` 占位不构造任何 HTTP 请求，直接返回错误；调用方应在
`Registry.Route` 之后立即用 `prov.Name()` 判断是否占位 —— 但这
不强制（占位错误由 provider 自身在 `Send` 时返回）。

### 10. 面板"站点管理" tab 的 DOM 形状

```html
<div class="panel" id="panel-sites">
  <div class="section">
    <div class="section-title">默认 provider</div>
    <select id="defaultProviderSelect"></select>
    <button onclick="App.saveDefaultProvider()">保存</button>
  </div>
  <div class="section" id="providerSections">
    <!-- 每个 provider 一个折叠面板，由 JS 动态渲染 -->
  </div>
</div>
```

JS 渲染逻辑：
```js
async loadSites() {
    const data = await (await fetch('/api/env')).json();
    const defaultProv = data.defaultProvider || 'yuanbao';
    const providers = data.providers || {};
    // 填充默认 provider 下拉框
    const sel = document.getElementById('defaultProviderSelect');
    sel.innerHTML = Object.keys(providers).map(n => 
        `<option value="${n}" ${n===defaultProv?'selected':''}>${n}</option>`
    ).join('');
    // 渲染每个 provider 的折叠面板
    const container = document.getElementById('providerSections');
    container.innerHTML = Object.entries(providers).map(([name, p]) => 
        renderProviderSection(name, p)
    ).join('');
}
```

每个 provider 折叠面板的 HTML（按 provider 类型决定 cookie UI）：
- yuanbao：`hy_token` + `hy_user` 两个 input（沿用现有）。
- qwen / kimi：单个 `cookie` input。

`saveProvider(name)` 函数读各 input 值，构造
`{ providers: { [name]: { enabled, cookie, agentId, maxConcurrency, ... } } }`
POST 到 `/api/config`。

### 11. 旧"配置" tab 的兼容

旧"配置" tab 的元宝字段（agentId、Cookie 双输入）保留为旧入口，
但其保存动作改为走新形态：
- `saveAgentId()` → POST `{ providers: { yuanbao: { agentId: ... } } }`
- `saveCookie()` → POST `{ providers: { yuanbao: { cookie: {...} } } }`
- `saveConcurrency()` → POST `{ providers: { yuanbao: { maxConcurrency, queueTimeoutSeconds, requestCooldownMs } } }`

旧 tab 仍然可见（作为快速入口），但页面顶部加一行小字提示：
"推荐使用'站点管理' tab 进行多 provider 配置"。

## 文件改动清单

### 新建
- `providers/provider.go` —— `Provider` 接口、`StreamChunk`、`ModelInfo`、
  `Message` / `Tool` / `RequestOptions` 类型、`Registry`。
- `providers/registry.go` —— 全局 `Registry` 初始化与路由实现。
- `providers/yuanbao/client.go` —— 迁自 `yuanbao/client.go`。
- `providers/yuanbao/provider.go` —— yuanbao `Provider` 实现 + `EffectiveYuanbaoCookie` 迁移。
- `providers/yuanbao/prompt.go` —— 从 `api/openai.go` / `api/anthropic.go` 抽出的 prompt 构造。
- `providers/yuanbao/provider_test.go` / `prompt_test.go`。
- `providers/qwen/client.go` / `provider.go` / `provider_test.go` —— 占位。
- `providers/kimi/client.go` / `provider.go` / `provider_test.go` —— 占位。

### 修改
- `api/openai.go` —— 改走 `provider.Registry().Route(model)`；
  `handleOpenAIStream` 接受 `ParseStreamLine` 函数参数。
- `api/anthropic.go` —— 同上。
- `api/config.go` —— `ServerConfigData` 加 `DefaultProvider`；
  `HandleSetConfig` 接受新形态。
- `api/config_persist.go` —— `RuntimeConfig` 新结构 + 自定义 `UnmarshalJSON`。
- `api/env.go` —— 新增 `defaultProvider` + `providers` 摘要。
- `api/ratelimit.go` —— `LimiterManager` + `For(name)`。
- `api/models.go` —— `HandleOpenAIModels` 改走 `Registry.All()`；
  删除 `MODEL_MAPPING` / `GetModelConfig` / `buildYuanbaoRequest`。
- `api/config_test.go` / `api/config_persist_test.go` —— 测试更新。
- `main.go` —— 初始化 `provider.Registry()` 与 `limiterManager`。
- `public/index.html` —— 新增"站点管理" tab。
- `public/app.js` —— `loadSites` / `saveProvider` / `saveDefaultProvider`。

### 删除
- `yuanbao/` 顶层目录（迁移完成且无引用后）。
- `api/env.go` 内旧的 cookie 读取路径（被 yuanbao provider 取代）。

## 数据流（处理一个 chat 请求）

```
client POST /v1/chat/completions
   │ { model: "qwen-max", messages: [...], tools: [...] }
   ▼
api.HandleOpenAIChatCompletion
   │ prov, _ := provider.Registry().Route("qwen-max")  // qwen provider
   │ rl := limiterManager.For("qwen")
   │ rl.Acquire / defer rl.Release
   │
   │ prompt, toolSystem, _ := prov.BuildPrompt(messages, tools)
   │ req, _ := prov.NewRequest(prompt, opts)
   │ resp, _ := prov.Send(req, agentID, convID)  // qwen: returns 501 error
   │
   ▼  // 真实场景下，qwen 占位会在这里返回 error
   501 / 400 / 200 (stream/non-stream)
```

## 失败模式与对策

- **未知 model** → `Registry.Route` 返回 error；handler 返回 400。
- **provider 停用** → 同上。
- **provider 占位**（qwen/kimi 在 `enabled=true` 时被调用）：
  `Send` 立即返回 error；handler 500 + error message 包含
  "not implemented"。前端收到错误后展示给用户。
- **runtime_config.json 旧形态**：通过 `UnmarshalJSON` 翻译为
  `Providers["yuanbao"]`；首次 `POST /api/config` 后写为新形态。
- **运行时改 defaultProvider**：当前请求仍按原 model 名路由（不重启）；
  改 defaultProvider 主要是影响 GET /v1/models 的默认模型与仪表盘
  顶层字段。**用户期望 defaultProvider 改动也走"保存后重启"模式**
  以保证一致性 → handler 响应中提示"已保存。点击'重启服务'按钮生效。"
- **LimiterManager 并发构造**：首次 `For(name)` 时 double-checked
  locking（`sync.Once` per name）保证只有一个 `RateLimiter` 被创建。

## 性能影响

- 模型路由 O(N) 遍历 provider 模型列表；N=3 且模型数 < 10 时开销可忽略。
- 限流器从单例变为 manager map，多一个 `sync.RWMutex` 读锁；不影响热路径。
- 每个 provider 第一次请求多一次 `os.Stat`/env 读取（构造 limiter）；
  后续零开销。
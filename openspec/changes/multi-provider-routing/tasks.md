# 任务：multi-provider-routing

实施清单。涉及 provider 抽象 + 多 provider 路由 + 限流器 manager 化 +
配置 schema 升级 + 面板新 tab + qwen/kimi 占位。约 14 个文件改动，42 个
checkbox。

## 1. Provider 抽象与 Registry

- [x] 1.1 新增 `providers/provider.go`：`Provider` 接口、`StreamChunk`、
      `ModelInfo`、`Message`、`Tool`、`RequestOptions` 类型。
- [x] 1.2 新增 `providers/registry.go`：全局 `Registry`、
      `Register/Get/All/Names/Default/SetDefault/Route` 方法。
- [x] 1.3 实现 `Route(modelName)` 三步算法：默认 provider 优先 →
      跨 provider 查找 → 失败返回 error。
- [ ] 1.4 在 `main.go` 启动时 `Register` 三个 provider 并 `SetDefault("yuanbao")`。
- [x] 1.5 单元测试 `TestRegistry_Route`：覆盖默认命中、跨 provider、
      未知 model、停用 provider 四种场景。

## 2. 迁 yuanbao → providers/yuanbao/

- [x] 2.1 把 `yuanbao/client.go` 整体迁到 `providers/yuanbao/client.go`，
      包名 `yuanbao` 保留。
- [x] 2.2 新增 `providers/yuanbao/provider.go`：实现 `Provider` 接口
      的 6 个方法；`Name = "yuanbao"`；`Models()` 至少含 `deep_seek_v3`、
      `hunyuan` 两个 ModelInfo。
- [ ] 2.3 把 `api/config.go` 的 `EffectiveYuanbaoCookie()` + `EffectiveYuanbaoCookieSource()`
      + `CookieSource*` 迁移到 `providers/yuanbao/provider.go`（或单独
      `cookie.go`），包内方法。
- [x] 2.4 yuanbao provider 的 `BuildPrompt` 把 `api/openai.go` 当前的
      `utils.ConvertMessagesToYuanbaoPrompt` 包装调用；
      `Anthropic` 风格单独写一个 build。
- [x] 2.5 yuanbao provider 的 `NewRequest` 包装 `buildYuanbaoRequest`。
- [x] 2.6 yuanbao provider 的 `ParseStreamLine` 把当前 `YuanbaoResponseChunk`
      翻译为 `StreamChunk{Type, Content, Text}`（`Msg` → `Text`）。
- [x] 2.7 yuanbao provider 的 `Send` 包装 `Client.SendRequestWithID`。
- [x] 2.8 单元测试 `TestYuanbaoProvider_*`：Name、Models、BuildPrompt、
      ParseStreamLine（含 think/text 两种）、Send 错误传播。

## 3. qwen / kimi 占位

- [x] 3.1 新增 `providers/qwen/client.go`：最小 `Client` 结构（`BaseURL`、
      `Headers` 占位即可，无需实现）。
- [x] 3.2 新增 `providers/qwen/provider.go`：`Name = "qwen"`；
      `Models()` 含 `qwen-max`、`qwen-plus`、`qwen-turbo`、`qwen-long`；
      `BuildPrompt` / `NewRequest` / `Send` 全部返回
      `"qwen provider is not yet implemented"` 错误；
      `ParseStreamLine` 返回 `(nil, nil)`。
- [x] 3.3 单元测试 `TestQwenProvider_NotImplemented`：每个方法各跑一次
      确认返回预期 error / nil。
- [x] 3.4 新增 `providers/kimi/client.go` / `provider.go` / `provider_test.go`：
      `Name = "kimi"`；`Models()` 含 `kimi-k2`、`moonshot-v1-128k`；
      行为同 qwen。

## 4. LimiterManager

- [ ] 4.1 改 `api/ratelimit.go`：删除 `globalRateLimiter` 进程级单例，
      引入 `limiterManager *LimiterManager`。
- [ ] 4.2 `LimiterManager` 实现：
      - `For(name string) *RateLimiter`：懒构造 + `sync.Once` per name；
      - 未知 name 返回 pass-through limiter（`maxConcurrency = 1<<30`）。
- [ ] 4.3 `InitRateLimiter()` 改为 `InitLimiterManager()`：不再构造
      单例 `RateLimiter`，改为构造空 `LimiterManager`。
- [ ] 4.4 暴露 `GetLimiterManager()` 替代 `GetRateLimiter()`。
- [ ] 4.5 单元测试 `TestLimiterManager_For`：覆盖每个 provider 独立
      limiter、未知 name pass-through、并发首次构造只触发一次。

## 5. RuntimeConfig 升级

- [ ] 5.1 `api/config_persist.go`：`RuntimeConfig` 改为
      `{Providers map[string]ProviderConfig, DefaultProvider string}`。
- [ ] 5.2 新增 `ProviderConfig` 结构：`Enabled, Cookie *YuanbaoCookie,
      AgentID *string, MaxConcurrency, QueueTimeoutSeconds,
      RequestCooldownMs *int`，全部带 `omitempty`。
- [ ] 5.3 实现 `RuntimeConfig.UnmarshalJSON` 双形态（详 design.md §4）。
- [ ] 5.4 单元测试 `TestRuntimeConfig_LegacyFields`：旧形态
      `{maxConcurrency, yuanbaoCookie: {...}}` 加载后归到
      `Providers["yuanbao"]`。
- [ ] 5.5 单元测试 `TestRuntimeConfig_NewForm`：新形态 round-trip。

## 6. HandleSetConfig 双形态

- [ ] 6.1 `api/config.go`：`ServerConfigData` 增加 `DefaultProvider` 字段。
- [ ] 6.2 `HandleSetConfig`：先检查 `body["providers"]`，存在则按
      新形态解析；否则检查 `body["yuanbaoCookie"]` 或
      `body["maxConcurrency"]` 等旧字段，存在则翻译为
      `Providers["yuanbao"]`；都没有则保留旧扁平字段（deepThinking 等）
      逻辑。
- [ ] 6.3 新形态解析：每条 provider 配置按现有规则校验（cookie 必须
      object、concurrency 范围等）。
- [ ] 6.4 旧形态翻译：旧字段归 `Providers["yuanbao"]`，`Enabled` 默认
      true，`DefaultProvider = "yuanbao"`（仅当旧请求体明确指定
      `defaultProvider` 时才覆盖）。
- [ ] 6.5 持久化路径：把 `Providers` 与 `DefaultProvider` 写入
      `RuntimeConfig`，复用既有 `SaveRuntimeConfig` 流程。
- [ ] 6.6 单元测试 `TestHandleSetConfig_*`：覆盖新形态保存、旧形态翻译、
      双形态 no-op、类型错误 400、新形态下 Providers 缺省字段 no-op。

## 7. /api/env 升级

- [ ] 7.1 `api/env.go`：响应增加 `defaultProvider`（string）与
      `providers`（object：每 provider 包含 `name, enabled,
      cookieSource, yuanbaoCookie, yuanbaoHyToken, yuanbaoHyUser`）。
- [ ] 7.2 保留旧顶层 `yuanbaoCookie` / `yuanbaoHyToken` /
      `yuanbaoHyUser` / `cookieSource`（取自 `defaultProvider`）。
- [ ] 7.3 单元测试 `TestHandleEnv_MultiProvider`：覆盖多 provider
      摘要、env 兜底来源报告。

## 8. /api/status 升级

- [ ] 8.1 `api/config.go` 的 `HandleStatus`：响应改为
      `{ providers: { <name>: { maxConcurrency, inflight, waiting,
      requestCooldownMs, queueTimeoutSeconds } }, maxConcurrency,
      inflight, waiting, requestCooldownMs }`。
- [ ] 8.2 顶层 stats 取自 `defaultProvider`。
- [ ] 8.3 单元测试 `TestHandleStatus_MultiProvider`。

## 9. /v1/models 升级

- [ ] 9.1 `api/models.go` 的 `HandleOpenAIModels` 改为遍历
      `provider.Registry().All()`；仅输出 `enabled` provider 的 `Models()`。
- [ ] 9.2 每个 ModelInfo 的 `ownedBy` 字段由 provider 提供（已在
      `provider.ModelInfo` 内）。
- [ ] 9.3 删除 `MODEL_MAPPING` / `GetModelConfig` / `buildYuanbaoRequest`
      （已迁到 yuanbao provider 包内）。
- [ ] 9.4 单元测试 `TestHandleOpenAIModels_*`：单 provider、多 provider
      并集、停用 provider 过滤、占位 provider 在停用时不出现在响应。

## 10. Handler 改走 Registry

- [ ] 10.1 `api/openai.go` 的 `HandleOpenAIChatCompletion`：用
      `provider.Registry().Route(req.Model)` 替代 `GetModelConfig` +
      `yuanbao.NewClient().SendRequestWithID`。
- [ ] 10.2 `rl := limiterManager.For(prov.Name())` 替代 `GetRateLimiter()`。
- [ ] 10.3 `handleOpenAIStream` 接受 `func(line string) (*provider.StreamChunk, error)`
      作为参数，替代硬编码 `yuanbao.ParseStreamLine`。
- [ ] 10.4 `api/anthropic.go` 同样改造。
- [ ] 10.5 集成测试（或手测说明）：用 `deep_seek_v3` 走完整流程仍然
      返回 yuanbao 风格响应；用 `qwen-max` 走流程返回 501 错误
      且 cookieSource 仍为 "env"（未启用则无 cookie）。

## 11. 删除 yuanbao/ 顶层目录

- [ ] 11.1 确认 `api/` 没有任何 import `"yuanbao2api/yuanbao"` 残留
      （除 `providers/yuanbao` 间接引用）。
- [ ] 11.2 删除 `yuanbao/` 目录（仅保留 `providers/yuanbao/`）。
- [ ] 11.3 `go build ./...` 通过。

## 12. 面板"站点管理" tab

- [ ] 12.1 `public/index.html`：在 tab-bar 末尾追加
      `<div class="tab" data-panel="sites">🗂 站点管理</div>`。
- [ ] 12.2 `public/index.html`：在 panel 列表末尾追加
      `<div class="panel" id="panel-sites">` 容器（含默认 provider
      下拉框与 `providerSections` 容器）。
- [ ] 12.3 `public/app.js`：新增 `loadSites()`：调 `GET /api/env`，
      渲染默认 provider 下拉框 + 每个 provider 的折叠面板（按 provider
      名称决定 cookie UI：yuanbao 用 hy_token/hy_user 双输入，qwen/kimi
      用单输入）。
- [ ] 12.4 `public/app.js`：新增 `saveProvider(name)`：读各 input
      值，构造新形态 POST `/api/config`。
- [ ] 12.5 `public/app.js`：新增 `saveDefaultProvider()`：调
      `POST /api/config { defaultProvider: <name> }`。
- [ ] 12.6 `public/app.js`：tab 切换逻辑：当切到 `sites` 时调用
      `loadSites()`。

## 13. 旧"配置" tab 适配新形态

- [ ] 13.1 `public/app.js` 的 `saveAgentId`：POST
      `{ providers: { yuanbao: { agentId } } }`。
- [ ] 13.2 `public/app.js` 的 `saveCookie`：POST
      `{ providers: { yuanbao: { cookie: {...} } } }`。
- [ ] 13.3 `public/app.js` 的 `saveConcurrency`：POST
      `{ providers: { yuanbao: { maxConcurrency, queueTimeoutSeconds,
      requestCooldownMs } } }`。
- [ ] 13.4 在旧"配置" tab 顶部加一行小字："推荐使用'站点管理' tab
      进行多 provider 配置"。

## 14. 端到端验证

- [ ] 14.1 `go build ./...` 通过。
- [ ] 14.2 `go test ./... -count=1` 全部通过（含新 Provider 抽象、
      Registry 路由、LimiterManager、RuntimeConfig 双形态、Handler 双形态、
      /api/env 升级、面板 JS 至少 smoke 测试）。
- [ ] 14.3 `go vet ./...` 无告警。
- [ ] 14.4 `openspec validate multi-provider-routing --type change --json`
      通过。
- [ ] 14.5 浏览器手测（运维执行）：
      - 启动服务 → 切到"站点管理" tab → 看 yuanbao/qwen/kimi 三节
      - 在 yuanbao 节保存 hy_token+hy_user → 重启 → `/v1/chat/completions` 正常
      - 启用 qwen → `/v1/models` 出现 qwen 模型 → 调 qwen 模型 → 收到 501 "not implemented"
      - 切 defaultProvider → `/v1/models` 顶层 stats 不变（取 defaultProvider）
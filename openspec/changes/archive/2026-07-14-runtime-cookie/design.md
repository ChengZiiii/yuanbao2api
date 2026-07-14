# 设计：runtime-cookie

## 架构决策

### 1. 指针类型字段 `YuanbaoCookie`

`RuntimeConfig.YuanbaoCookie *string`（json tag `yuanbaoCookie,omitempty`），
`ServerConfigData` 中同样采用指针类型。

**理由**。现有 `RuntimeConfig` 已使用指针字段（`MaxConcurrency *int` 等），
用于区分"JSON 中缺省"与"显式为零"。这里沿用同一惯例，
让 `HandleSetConfig` 的部分更新语义保持一致：
POST 请求体里缺省 `yuanbaoCookie` 时不得清空此前保存的 Cookie。

### 2. 单点解析助手

在 `api/config.go` 新增函数 `EffectiveYuanbaoCookie() string`，
按以下优先级返回 Cookie：

```
runtime（RuntimeConfig.YuanbaoCookie） → env（YUANBAO_COOKIE） → ""
```

所有调用方——`yuanbao/client.go`、`HandleEnv`——都走这个助手。
本 change 完成后，代码库里 SHALL 不再有直接读
`os.Getenv("YUANBAO_COOKIE")` 的位置。

**理由**。集中解析便于将来扩展优先级（例如增加加密文件源），
不必再审计全代码库；同时让"运行时语义"一目了然——每次调用都重新解析，
未来要做的"运行中热更新 RuntimeConfig"路径也能直接复用。

### 3. 客户端每次请求读取 Cookie，不在构造时绑定

`yuanbao.Client` 当前在 `NewClient()` 里把 `Config.Cookies string` 填好。
本 change 后：

- 删除 `Config.Cookies` 字段。
- `NewClient()` 不再访问 env。
- `SendRequestWithID` 在写请求头前调用 `EffectiveYuanbaoCookie()`。

**理由**。运维 UX 是"保存→重启→新 Cookie 生效"。
也可以让 `NewClient()` 读快照、依赖重启重建客户端，
但那样 Cookie 来源就和构造点耦合，将来如果要做
"401 时自动轮换 Cookie"这种鉴权路径会很别扭。
每次请求解析的成本是一次指针解引用 + 一次 `os.Getenv`（后者被 `os`
包缓存），相比上游 HTTP 调用可以忽略不计。

### 4. `Cookie` 头的写入语义

原 `SendRequestWithID`：

```go
if c.Config.Cookies != "" {
    req.Header.Set("cookie", c.Config.Cookies)
} else {
    req.Header.Set("cookie", os.Getenv("YUANBAO_COOKIE"))
}
```

本 change 后：

```go
if cookie := EffectiveYuanbaoCookie(); cookie != "" {
    req.Header.Set("cookie", cookie)
}
```

不再对空 Cookie 调用 `req.Header.Set("cookie", "")`。
Go 的 `net/http` 本来就会丢弃空 header 显式不写更清晰，也与包内
其它代码风格一致。

### 5. `runtime_config.json` 的写入语义

`SaveRuntimeConfig` 始终把整个 `RuntimeConfig` 结构体序列化写入。
`HandleSetConfig` 当前用 `serverConfig.MaxConcurrency` /
`QueueTimeoutSeconds` / `RequestCooldownMs` 构造待写入结构体，
本 change 增加 `YuanbaoCookie`。成功保存即覆盖该字段。

为了保留"POST 缺省字段即 no-op"的语义，`HandleSetConfig` 仅在
**任意一个运行时字段发生变化**时才调用 `SaveRuntimeConfig`。
Cookie 指针仅在请求显式设置时写入结构体；如果运维从未碰过 Cookie，
磁盘上的旧值保持不变。

**理由**。让指针字段（可能为 `nil`，用于"清除"）整体往返，
"用空串清除 Cookie"这条需求就无需额外的"clear"标志。

### 6. 现有 `runtime_config.json` 向后兼容

带 `omitempty` 的指针字段在 JSON 缺省键时反序列化为 `nil`，
所以现网在用的 `runtime_config.json` 无需修改即可加载。
本 change 后的首次保存会在文件中追加 `yuanbaoCookie` 键
（或在运维从未提供时保持缺省）。

## 文件改动清单

### `api/config_persist.go`
- `RuntimeConfig` 增加 `YuanbaoCookie *string`，json tag
  `yuanbaoCookie,omitempty`。
- `SaveRuntimeConfig` / `LoadRuntimeConfig` 不动：它们已通过
  `encoding/json` 整体处理结构体。

### `api/config.go`
- `ServerConfigData` 增加 `YuanbaoCookie *string`，json tag
  `yuanbaoCookie`。
- `HandleSetConfig`：
  - 解析可选 `yuanbaoCookie`（必须为字符串，否则 400）；
  - 写回 `serverConfig.YuanbaoCookie`（空串即 `nil`）；
  - 当任意运行时字段变化时调用 `SaveRuntimeConfig` 持久化。
- 新增 `EffectiveYuanbaoCookie() string`：唯一解析入口。
- `HandleEnv`：通过新助手读有效 Cookie，用 `maskCookie` 打码，
  响应里加 `cookieSource`。

### `yuanbao/client.go`
- 删除 `Config.Cookies string`。
- `NewClient()` 不再调 `os.Getenv("YUANBAO_COOKIE")`。
- `SendRequestWithID` 调用 `EffectiveYuanbaoCookie()`，
  非空时 `req.Header.Set("cookie", cookie)`。
- 删除本包内残余的 `os.Getenv("YUANBAO_COOKIE")` 兜底分支。

### `public/index.html`
- 在"运行时配置"一节、并发表单之上新增一行：
  - `<textarea id="yuanbaoCookieInput" rows="3">`（带可选显隐切换）；
  - `<button class="btn-sm" onclick="App.saveCookie()">保存 Cookie</button>`；
  - 与并发参数同款的提示行（"⚠ 修改后需重启服务才能生效"）。
- "环境变量一览"的 `YUANBAO_COOKIE` 行追加 `<span id="cookieSource">`
  单元格显示当前来源。

### `public/app.js`
- 扩展 `loadEnv()`，读取 `data.cookieSource` 并写入 `#cookieSource`。
- 新增 `saveCookie()`：POST `{ yuanbaoCookie: <value> }` 至 `/api/config`，
  成功后展示与 `saveConcurrency()` 相同的重启提示。
- 给 `<textarea>` 加上"显示/隐藏"切换，默认隐藏。

### `api/config_persist_test.go`
- 增加 `YuanbaoCookie` 经 `SaveRuntimeConfig` → `LoadRuntimeConfig`
  的往返测试（用 `RUNTIME_CONFIG_PATH` 隔离）。
- 增加测试：缺失 `yuanbaoCookie` 键的旧 `runtime_config.json`
  加载后 `YuanbaoCookie == nil`。
- 若存在 `HandleSetConfig` 测试则扩展覆盖新字段；
  否则用 `httptest` 新增一组 handler 测试。

## 数据流

```
运维在面板粘贴 Cookie
   │
   ▼
public/app.js saveCookie()
   │ POST /api/config  { yuanbaoCookie: "abc..." }
   ▼
api.HandleSetConfig
   │ serverConfig.YuanbaoCookie = &"abc..."
   │ SaveRuntimeConfig({ YuanbaoCookie: &"abc...", ... })
   ▼
磁盘上 runtime_config.json  { ..., "yuanbaoCookie": "abc..." }

运维点击"🔄 重启服务"
   │ POST /api/restart
   ▼
进程退出；restart.bat 重新拉起
   │
   ▼
main() → godotenv.Load() → api.InitRateLimiter() → ...
   │
   ▼
首个聊天请求命中 /v1/chat/completions
   │
   ▼
api.HandleOpenAIChatCompletion
   │ yuanbao.NewClient().SendRequestWithID(...)
   ▼
yuanbao.SendRequestWithID
   │ cookie := EffectiveYuanbaoCookie()
   │      runtime:    runtime_config.json:yuanbaoCookie = "abc..."
   │      env 兜底被跳过（runtime 已设）
   │ req.Header.Set("cookie", "abc...")
   ▼
上游元宝收到有效 Cookie。
```

## 失败模式与对策

- **运维用空串清 Cookie，但 env 也未设。** 后续请求不带 `Cookie` 头，
  上游返回 401/4xx。API 层已会把上游错误按 status + body 上抛，
  可观测。该场景已在 spec 中"两者皆空"场景显式记录。
- **`runtime_config.json` 损坏。** `LoadRuntimeConfig` 已对 JSON
  反序列化错误返回零值，并把"文件不存在"视为"无覆盖"。
  损坏文件等价于禁用运行时覆盖，自动回落至 env——与改造前行为一致，
  本 change 不引入额外处理。
- **运维触发的重启未完成。** `app.js` 现成的 `restartService()`
  轮询 `/health` 最长 30s，超时报错；本 change 复用即可。
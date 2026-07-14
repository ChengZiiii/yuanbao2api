# 设计：yuanbao-cookie-fields

## 架构决策

### 1. 结构体定义与自定义 UnmarshalJSON

```go
// YuanbaoCookie 持有元宝 session cookie 的两部分。
// HeaderValue 按 "hy_token=...; hy_user=..." 格式组装。
type YuanbaoCookie struct {
    HyToken string `json:"hyToken"`
    HyUser  string `json:"hyUser"`
}

// UnmarshalJSON 同时接受结构体与字符串两种形态。
// 字符串形态仅用于向后兼容 runtime-cookie 期间的旧数据。
func (c *YuanbaoCookie) UnmarshalJSON(data []byte) error {
    // 1) 尝试结构体
    type alias YuanbaoCookie
    var s alias
    if err := json.Unmarshal(data, &s); err == nil {
        *c = YuanbaoCookie(s)
        return nil
    }
    // 2) 回退字符串："hy_token=xxx; hy_user=yyy"
    var str string
    if err := json.Unmarshal(data, &str); err != nil {
        return err
    }
    return c.parseLegacyString(str)
}

func (c *YuanbaoCookie) parseLegacyString(s string) error {
    for _, pair := range strings.Split(s, ";") {
        pair = strings.TrimSpace(pair)
        idx := strings.Index(pair, "=")
        if idx <= 0 {
            continue
        }
        key := strings.TrimSpace(pair[:idx])
        val := strings.TrimSpace(pair[idx+1:])
        switch key {
        case "hy_token":
            c.HyToken = val
        case "hy_user":
            c.HyUser = val
        }
    }
    return nil
}

// HeaderValue 组装 Cookie 头。
func (c *YuanbaoCookie) HeaderValue() string {
    if c == nil {
        return ""
    }
    var parts []string
    if c.HyToken != "" {
        parts = append(parts, "hy_token="+c.HyToken)
    }
    if c.HyUser != "" {
        parts = append(parts, "hy_user="+c.HyUser)
    }
    return strings.Join(parts, "; ")
}
```

**理由**。结构体形式是新的标准形态，序列化始终输出结构体（无需自定义
`MarshalJSON`）。`UnmarshalJSON` 同时接受旧字符串，让现有部署的
`runtime_config.json` 在不手改的情况下能被加载 —— `LoadRuntimeConfig`
对 JSON 错误的兜底语义（已由 `fix-windows-rename` 锁定）保持不变。

### 2. 指针语义不变

`RuntimeConfig.YuanbaoCookie` 与 `ServerConfigData.YuanbaoCookie` 仍为
`*YuanbaoCookie`（指针类型）：
- `nil` → 缺省（回落至 env）
- `&{}`（零值结构体） → 显式"全空"，但 `EffectiveYuanbaoCookie()`
  内部会再判断 `HyToken == "" && HyUser == ""`，与 `nil` 等价。

为简化，`HandleSetConfig` 在收到空对象时把指针设为 `nil`（清空），
与 `runtime-cookie` 时的"`""` → nil" 行为一致。

### 3. `EffectiveYuanbaoCookie()` 实现调整

```go
func EffectiveYuanbaoCookie() string {
    serverConfigLock.RLock()
    yc := serverConfig.YuanbaoCookie  // *YuanbaoCookie or nil
    serverConfigLock.RUnlock()
    if yc != nil {
        if h := yc.HeaderValue(); h != "" {
            return h
        }
    }
    return os.Getenv("YUANBAO_COOKIE")
}
```

`EffectiveYuanbaoCookieSource()` 同理：检查 `yc != nil && (HyToken != "" || HyUser != "")`。

### 4. 请求体校验

`HandleSetConfig` 收到 `body["yuanbaoCookie"]` 时：
- `nil`/缺省 → no-op（与既有 `deepThinking` 等一致）。
- 非 object → HTTP 400 "yuanbaoCookie must be an object"。
- `map[string]interface{}` → 取 `hyToken` 与 `hyUser`，必须为字符串或缺省；
  非字符串 → HTTP 400。
- 构造 `*YuanbaoCookie{HyToken: ..., HyUser: ...}`：
  - 两字段都为空字符串或都缺省 → 设 `serverConfig.YuanbaoCookie = nil`。
  - 否则 → 设 `serverConfig.YuanbaoCookie = &YuanbaoCookie{...}`。

### 5. `/api/env` 响应字段

```json
{
  "yuanbaoCookie": "hy_toke****",
  "yuanbaoHyToken": "abcdef1****",
  "yuanbaoHyUser": "uvwxyz****",
  "cookieSource": "runtime",
  ...
}
```

- `yuanbaoCookie`：拼装后字符串的前 8 位 + `****`，保留向后兼容。
- `yuanbaoHyToken` / `yuanbaoHyUser`：分项掩码。空时为 `""`。
- `cookieSource`：基于优先级 1 是否命中。

### 6. 面板 UI

两个 `<input type="password">` 输入框：
- `id="yuanbaoHyTokenInput"`、`id="yuanbaoHyUserInput"`
- 默认隐藏（type=password）。
- 共用一个"显示 Cookie"切换按钮，点击后两个都改为 `type=text`。
- 输入框上方一行小字说明："元宝 Cookie 由 hy_token 和 hy_user 两部分组成，
  请分别粘贴"。

`saveCookie()`：
- 读两个输入框的值；
- POST `{ yuanbaoCookie: { hyToken: v1, hyUser: v2 } }`。

`loadEnv()`：
- 同时把 `yuanbaoHyToken` / `yuanbaoHyUser` / `yuanbaoCookie` / `cookieSource`
  写入对应 DOM 节点。

### 7. 不修改 `yuanbao/client.go`

`EffectiveYuanbaoCookie()` 仍返回字符串（拼装好的 Cookie 头）。
`CookieResolver` 委托不需调整。

## 文件改动清单

### `api/config_persist.go`
- 新增 `YuanbaoCookie` 结构体（含 `UnmarshalJSON` 与 `parseLegacyString`
  私有方法）；
- 新增 `(c *YuanbaoCookie) HeaderValue() string` 方法；
- `RuntimeConfig.YuanbaoCookie` 改为 `*YuanbaoCookie`。

### `api/config.go`
- `ServerConfigData.YuanbaoCookie` 改为 `*YuanbaoCookie`；
- `EffectiveYuanbaoCookie()` 改用 `HeaderValue()`；
- `EffectiveYuanbaoCookieSource()` 检查 `HyToken` / `HyUser` 任一非空；
- `HandleSetConfig`：
  - 解析 `yuanbaoCookie` 为 object（`map[string]interface{}`）；
  - 非 object → 400；
  - 字段类型校验；
  - 全空 → `nil`；否则构造指针并赋值；
  - 持久化路径不变。

### `api/env.go`
- 增加 `yuanbaoHyToken`、`yuanbaoHyUser` 两个掩码字段；
- `yuanbaoCookie` 改为基于 `HeaderValue()` 的掩码（如果运行时非空），
  否则保留 env 字符串掩码。

### `api/config_persist_test.go`
- 既有 `TestSaveAndLoadRuntimeConfig_RoundTripYuanbaoCookie`：
  改用结构体形式；
- 新增 `TestLoadRuntimeConfig_LegacyYuanbaoCookieString`：
  旧 `"hy_token=...; hy_user=..."` 字符串能被解析为结构体字段。

### `api/config_test.go`
- 既有 4 个 `TestHandleSetConfig_*`：
  改用 `{ hyToken, hyUser }` 对象；
- 新增 `TestHandleSetConfig_RejectsNonObjectYuanbaoCookie`；
- 新增 `TestYuanbaoCookieHeaderValue`（覆盖 4 种组装场景）。

### `public/index.html`
- 替换 `<textarea id="yuanbaoCookieInput">` 为两个
  `<input type="password" id="yuanbaoHyTokenInput">` /
  `<input type="password" id="yuanbaoHyUserInput">`；
- 增加显隐切换按钮；
- "环境变量一览" 中 `YUANBAO_COOKIE` 行追加两个 `<span>` 显示分项掩码。

### `public/app.js`
- `loadEnv()` 写 `yuanbaoHyToken` / `yuanbaoHyUser` / `yuanbaoCookie` /
  `cookieSource`；
- `saveCookie()` 读两个 input，发对象形态；
- 新增 `toggleCookieVisibility(visible)`。

## 数据流

```
面板: 用户在两个 input 填好 hy_token 与 hy_user 的值
   │
   ▼
public/app.js saveCookie()
   │ POST /api/config
   │   { yuanbaoCookie: { hyToken: "...", hyUser: "..." } }
   ▼
api.HandleSetConfig
   │ 解析对象，构造 serverConfig.YuanbaoCookie = &{...}
   │ SaveRuntimeConfig({ YuanbaoCookie: &{...}, ... })
   ▼
磁盘 runtime_config.json
   │ { ..., "yuanbaoCookie": {"hyToken":"...","hyUser":"..."} }

运维点"🔄 重启服务"
   │ POST /api/restart
   ▼
进程退出；restart.bat 拉起
   │
   ▼
首个 /v1/chat/completions 请求
   │
   ▼
yuanbao.SendRequestWithID
   │ cookie := EffectiveYuanbaoCookie()
   │      命中 runtime: HeaderValue() = "hy_token=<tok>; hy_user=<usr>"
   │      回落 env:       "env-cookie"
   │ req.Header.Set("cookie", cookie)
   ▼
上游元宝收到合法 Cookie。
```

## 失败模式与对策

- **JSON 反序列化失败**（极端的旧数据形态）：`UnmarshalJSON` 两种形态
  都尝试；都失败时向上抛错。`LoadRuntimeConfig` 兜底语义把它当成
  "无覆盖"，回落至 env。
- **运行时 Cookie 被构造为全空对象**（用户在面板把两个框都留空并保存）：
  `HandleSetConfig` 把 `YuanbaoCookie` 设为 `nil`，与"清除"语义一致。
  `EffectiveYuanbaoCookie()` 后续回落至 env。
- **Yuanbao 服务端要求两段顺序固定**：按 spec，组装顺序固定为
  `hy_token` 在前、`hy_user` 在后。如服务端实际相反，需要另开 change。
- **面板双 input 都没填、点保存**：等同"清除运行时覆盖"，服务端
  把 `YuanbaoCookie` 设为 `nil`。

## 性能影响

无。每次请求仍然是一次指针解引用 + 一次字符串拼接（双字段时是
`strings.Join`，无明显开销）。`UnmarshalJSON` 只在 `LoadRuntimeConfig`
启动时执行一次。
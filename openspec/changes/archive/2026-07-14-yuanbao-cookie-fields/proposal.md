# 提案：yuanbao-cookie-fields

## Why

`runtime-cookie` change 把元宝 Cookie 落地为面板里一个多行文本框（`textarea`），
让用户直接粘贴完整 HTTP `Cookie` 头字符串（形如
`hy_token=xxx; hy_user=yyy`）。

实际运维场景中：
- 用户从浏览器复制出来的通常是分开的两个值（DevTools 的 "Copy value"
  一次只能拿到一个）；
- 让用户手敲 `hy_token=`、`hy_user=` 这两个 key 名既繁琐又容易拼错。

我们希望面板提供两个独立输入框（"Token" / "User"），用户在每个框
只填值；服务端把两个值拼装成完整的 Cookie 头字符串后再写入出站请求。

## What Changes

- `RuntimeConfig.YuanbaoCookie` 从 `*string` 改为
  `*YuanbaoCookie{HyToken, HyUser string}`。`ServerConfigData` 同步。
- `YuanbaoCookie` 类型带自定义 `UnmarshalJSON`，同时接受：
  1. **结构体形式** `{"hyToken":"...","hyUser":"..."}`（本 change 后的
     新写入格式）；
  2. **字符串形式** `"hy_token=xxx; hy_user=yyy"`（`runtime-cookie`
     期间可能落地的旧数据）—— 解析时识别 `hy_token=` 与 `hy_user=`
     两个 key，把对应值填入字段。
  序列化（`MarshalJSON`）一律输出结构体形式 —— 不再回写字符串形式。
- `EffectiveYuanbaoCookie()` 内部把 `YuanbaoCookie` 拼成
  `hy_token=<HyToken>; hy_user=<HyUser>` 返回；
  - 任一字段为空时省略对应段；
  - 两字段都为空时返回 `""`。
- `POST /api/config` 接受 `yuanbaoCookie` 为对象 `{hyToken, hyUser}`；
  校验：必须是 object；两字段都为空字符串或两字段都缺省时视为"清除"
  （保存为 `nil`）；非 object 返回 HTTP 400。
- `GET /api/env`：
  - 保留现有 `yuanbaoCookie` 字段（mask 后的拼装字符串，向后兼容
    旧前端代码）；
  - 新增 `yuanbaoHyToken`（HyToken 前 8 位 + `****`，空时为 `""`），
    `yuanbaoHyUser`（同上）；
  - `cookieSource` 语义保持不变。
- 面板配置页"运行时配置"区：
  - 旧 `<textarea id="yuanbaoCookieInput">` 替换为两个独立输入框
    `<input id="yuanbaoHyTokenInput">` 与 `<input id="yuanbaoHyUserInput">`；
  - "保存 Cookie" 按钮逻辑改为 POST `{ yuanbaoCookie: { hyToken, hyUser } }`；
  - 增加显隐切换（默认隐藏）；
  - "环境变量一览" 中 `YUANBAO_COOKIE` 行展示 `cookieSource` 之外，
    附两个分项掩码值。
- 单元测试更新：
  - 既有 cookie round-trip / 优先级测试改用新结构体；
  - 新增"旧字符串 → 新结构体"反序列化兼容测试；
  - 新增"Cookie 头拼装"测试（覆盖三态：双字段、单字段、全空）。

## Impact

- 受影响 spec：`configuration` —— 5 条既有 Requirement 全部 MODIFIED
  （数据模型 / 解析 / API / 观测 / UI 全部变化），并新增 1 条
  "Cookie 头拼装" Requirement。
- 受影响文件：
  - `api/config_persist.go`（结构体字段类型）
  - `api/config.go`（`ServerConfigData` 字段、handler、拼装助手）
  - `api/env.go`（双字段掩码）
  - `api/config_persist_test.go`、`api/config_test.go`（测试更新）
  - `yuanbao/client.go`（无需改动 —— `EffectiveYuanbaoCookie()` 仍返回字符串）
  - `public/index.html`、`public/app.js`（双输入框 + JS）
- 不引入新依赖。
- 旧的 `runtime_config.json` 文件（含 `"yuanbaoCookie":"hy_token=...; hy_user=..."`
  字符串）能通过自定义 `UnmarshalJSON` 自动迁移到结构体形式。

## Compatibility

- **磁盘上的旧 JSON**：`UnmarshalJSON` 接受两种形态，旧文件不会报错。
- **GET /api/env 响应**：保留 `yuanbaoCookie` 字段（拼好的掩码字符串），
  旧前端读 `data.yuanbaoCookie` 不受影响。
- **`EffectiveYuanbaoCookie()`**：返回类型与语义不变（始终是 Cookie 头字符串），
  `yuanbao/client.go` 无需改动。
- **POST /api/config 请求体**：从 `{"yuanbaoCookie":"..."}` 变为
  `{"yuanbaoCookie":{"hyToken":"...","hyUser":"..."}}`。同一台机器
  的前端会一起升级，无跨部署兼容问题。

## Approach（高层）

1. `api/config_persist.go`：新增 `YuanbaoCookie` 结构体（含自定义
   `UnmarshalJSON`），把 `RuntimeConfig.YuanbaoCookie` 改成
   `*YuanbaoCookie`。
2. `api/config.go`：
   - `ServerConfigData.YuanbaoCookie` 改为 `*YuanbaoCookie`；
   - 新增 `(c *YuanbaoCookie) HeaderValue() string` 助手，组装 Cookie
     头；
   - `EffectiveYuanbaoCookie()` 内部改用 `HeaderValue()`；
   - `EffectiveYuanbaoCookieSource()` 检查 `HyToken` 与 `HyUser`
     是否同时为空；
   - `HandleSetConfig` 解析新对象形态；非 object → 400；都空 → 清空；
   - 既有校验逻辑保持（缺省 → no-op）。
3. `api/env.go`：在原 `yuanbaoCookie` 之外，新增 `yuanbaoHyToken` 与
   `yuanbaoHyUser` 两个掩码字段。
4. `public/index.html`：替换 textarea 为两个 input。
5. `public/app.js`：`loadEnv` 读三个字段；`saveCookie` 读两个 input、
   发对象形态。
6. 测试更新 + 新增。
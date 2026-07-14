# 任务：yuanbao-cookie-fields

实施清单。涉及数据模型变更 + 前端 UI 调整，约 7 个文件改动。

## 1. 数据模型与持久化

- [x] 1.1 在 `api/config_persist.go` 新增 `YuanbaoCookie` 结构体（`HyToken`、
      `HyUser` 两个 string 字段，json tag `hyToken` / `hyUser`）。
- [x] 1.2 为 `YuanbaoCookie` 实现 `UnmarshalJSON`：
      - 先尝试结构体形态；
      - 失败则尝试字符串 `"hy_token=...; hy_user=..."`，解析后填入字段；
      - 两种都失败时向上抛错。
- [x] 1.3 实现 `(c *YuanbaoCookie) parseLegacyString(s string) error`
      私有方法（被 `UnmarshalJSON` 调用）。
- [x] 1.4 实现 `(c *YuanbaoCookie) HeaderValue() string` 公开方法：
      `hy_token=<HyToken>; hy_user=<HyUser>`，空字段省略。
- [x] 1.5 把 `RuntimeConfig.YuanbaoCookie` 字段类型改为 `*YuanbaoCookie`。
- [x] 1.6 新增 import：`strings`。

## 2. 服务端解析与 API

- [x] 2.1 `api/config.go`：把 `ServerConfigData.YuanbaoCookie` 字段类型
      改为 `*YuanbaoCookie`。
- [x] 2.2 `EffectiveYuanbaoCookie()` 改用 `HeaderValue()`：当
      `serverConfig.YuanbaoCookie != nil` 时返回其 `HeaderValue()`；
      非空则使用，否则回落至 `os.Getenv("YUANBAO_COOKIE")`。
- [x] 2.3 `EffectiveYuanbaoCookieSource()` 检查 `YuanbaoCookie` 非 nil
      且 `HyToken` / `HyUser` 任一非空。
- [x] 2.4 `HandleSetConfig`：解析 `yuanbaoCookie` 为 object：
      - 缺省 → no-op；
      - 非 object（字符串、数字、数组、null 之外） → HTTP 400；
      - 字段类型校验：`hyToken` / `hyUser` 必须为 string 或缺省；
      - 两字段都为空字符串或都缺省 → `serverConfig.YuanbaoCookie = nil`；
      - 否则构造 `&YuanbaoCookie{...}` 并赋值。
- [x] 2.5 持久化路径：把 `serverConfig.YuanbaoCookie`（可能为 nil）
      写入 `RuntimeConfig.YuanbaoCookie`，复用既有逻辑。

## 3. /api/env 响应

- [x] 3.1 `api/env.go`：新增 `yuanbaoHyToken` 与 `yuanbaoHyUser`
      两个掩码字段。
- [x] 3.2 `yuanbaoCookie` 字段保留：返回 `HeaderValue()`（如运行时非空）
      或 `os.Getenv("YUANBAO_COOKIE")`（回落时）的掩码。
- [x] 3.3 `cookieSource` 语义保持不变。

## 4. 单元测试

- [x] 4.1 `api/config_persist_test.go`：更新
      `TestSaveAndLoadRuntimeConfig_RoundTripYuanbaoCookie` 使用结构体。
- [x] 4.2 新增 `TestLoadRuntimeConfig_LegacyYuanbaoCookieString`：
      `"hy_token=legacy; hy_user=old"` 字符串加载后 `HyToken=="legacy"`、
      `HyUser=="old"`。
- [x] 4.3 新增 `TestLoadRuntimeConfig_ObjectYuanbaoCookie`：
      结构体形态 round-trip。
- [x] 4.4 `api/config_test.go`：更新既有 4 个
      `TestHandleSetConfig_*YuanbaoCookie*` 使用新对象形态。
- [x] 4.5 新增 `TestHandleSetConfig_RejectsNonObjectYuanbaoCookie`：
      字符串值 → 400。
- [x] 4.6 新增 `TestYuanbaoCookie_HeaderValue`：覆盖双字段、仅 token、
      仅 user、全空 4 种组装场景。
- [x] 4.7 新增 `TestEffectiveYuanbaoCookie_StructureBased`：
      验证新结构体下 EffectiveYuanbaoCookie 优先级。

## 5. 前端 UI

- [x] 5.1 `public/index.html`：在"运行时配置"段删除原
      `<textarea id="yuanbaoCookieInput">`，新增：
      - 一个小字说明行；
      - `<input type="password" id="yuanbaoHyTokenInput">`；
      - `<input type="password" id="yuanbaoHyUserInput">`；
      - 一个"显示 Cookie"切换按钮；
      - 保留"保存 Cookie"按钮。
- [x] 5.2 "环境变量一览" 中 `YUANBAO_COOKIE` 行追加
      `<span id="yuanbaoHyToken">` 与 `<span id="yuanbaoHyUser">`。
- [x] 5.3 `public/app.js`：`loadEnv()` 写入
      `yuanbaoHyToken` / `yuanbaoHyUser` / `yuanbaoCookie` / `cookieSource`
      四个 DOM 节点。
- [x] 5.4 `public/app.js`：重写 `saveCookie()`，读两个 input 后 POST
      `{ yuanbaoCookie: { hyToken: <v1>, hyUser: <v2> } }`。
- [x] 5.5 `public/app.js`：更新 `toggleCookieVisibility()` 同时切换
      两个 input 的 `type` 属性。

## 6. 端到端验证

- [x] 6.1 `go build ./...` 通过。
- [x] 6.2 `go test ./... -count=1` 全部通过（含新增与更新的测试）。
- [x] 6.3 `go vet ./...` 无告警。
- [x] 6.4 `openspec validate yuanbao-cookie-fields --type change --json`
      通过。
- [ ] 6.5 浏览器手测（运维执行）：启动服务 → 面板输入两个值 →
      保存 → 重启 → `/v1/chat/completions` 正常返回 → 验证
      `GET /api/env` 显示 `cookieSource: "runtime"` 与两个掩码值。
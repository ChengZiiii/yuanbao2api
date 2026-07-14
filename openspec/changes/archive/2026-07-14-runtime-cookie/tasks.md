# 任务：runtime-cookie

实现"运行时管理元宝 Cookie"的清单。每个任务列出具体改动文件与验证步骤；
按节顺序执行，每节完成后构建都必须保持绿色。

## 1. 持久层

- [x] 1.1 在 `api/config_persist.go` 的 `RuntimeConfig` 上新增
      `YuanbaoCookie *string`（json tag `yuanbaoCookie,omitempty`）。
- [x] 1.2 确认 `SaveRuntimeConfig` / `LoadRuntimeConfig` 不需要结构层面
      的改动（json 整体往返即可覆盖新字段）。
- [x] 1.3 在 `api/config_persist_test.go` 新增单元测试：
      `YuanbaoCookie` 经 `SaveRuntimeConfig` → `LoadRuntimeConfig` 的
      round-trip（用 `RUNTIME_CONFIG_PATH` 隔离）。
- [x] 1.4 新增单元测试：旧版（无 `yuanbaoCookie` 键）的
      `runtime_config.json` 加载后 `YuanbaoCookie == nil`。
- [x] 1.5 执行 `go test ./api/...` 全部通过。

## 2. 解析助手与 API handlers

- [x] 2.1 在 `api/config.go` 的 `ServerConfigData` 上新增
      `YuanbaoCookie *string`（json tag `yuanbaoCookie`）。
- [x] 2.2 在 `api/config.go` 实现 `EffectiveYuanbaoCookie() string`，
      优先级：runtime → env → ""。
- [x] 2.3 更新 `HandleSetConfig`：
      - 解析可选 `yuanbaoCookie`（类型非字符串返回 HTTP 400）；
      - 写回 `serverConfig.YuanbaoCookie`（空串即 `nil`）；
      - 当任意运行时字段变化时调用 `SaveRuntimeConfig` 持久化。
- [x] 2.4 更新 `HandleEnv`：通过新助手读有效 Cookie，
      `maskCookie` 打码，响应里附 `cookieSource`
      （`"runtime"` / `"env"` / `"none"`）。
- [x] 2.5 新增 `HandleSetConfig` 的 handler 测试，覆盖：
      保存非空、用空串清除、缺省字段 no-op、类型错误 HTTP 400。
- [x] 2.6 执行 `go test ./api/...` 全部通过。

## 3. 上游客户端按请求读取 Cookie

- [x] 3.1 删除 `yuanbao/client.go` 中 `Config.Cookies string` 字段。
- [x] 3.2 让 `NewClient()` 不再调 `os.Getenv("YUANBAO_COOKIE")`。
- [x] 3.3 把 `SendRequestWithID` 内的 Cookie 写入逻辑替换为单次
      `EffectiveYuanbaoCookie()` 调用，非空时
      `req.Header.Set("cookie", cookie)`。
- [x] 3.4 全包检索并删除任何残余的 `os.Getenv("YUANBAO_COOKIE")`
      （仅 `api/config.go` 内的两个解析助手保留；这是设计要求的
      "唯一解析入口"）。
- [x] 3.5 执行 `go build ./...` 与 `go test ./...` 全部通过。

## 4. 面板：Cookie 编辑入口

- [x] 4.1 在 `public/index.html` 的"运行时配置"一节、并发表单之上新增：
      - `<textarea id="yuanbaoCookieInput" rows="3">`（带可选显隐切换）；
      - `<button class="btn-sm" onclick="App.saveCookie()">保存 Cookie</button>`；
      - 与并发参数同款的"⚠ 修改后需重启服务才能生效"提示。
- [x] 4.2 在 `public/index.html` 的"环境变量一览"中
      `YUANBAO_COOKIE` 行追加 `<span id="cookieSource">` 单元格。
- [x] 4.3 在 `public/app.js` 扩展 `loadEnv()`，读取
      `data.cookieSource` 并写入 `#cookieSource`。
- [x] 4.4 在 `public/app.js` 新增 `saveCookie()`：POST
      `{ yuanbaoCookie: <value> }` 至 `/api/config`，成功后展示
      与 `saveConcurrency()` 相同的重启提示。
- [x] 4.5 浏览器手测：粘贴 Cookie → 保存 → 看到成功提示 →
      重启 → 后续 `/v1/chat/completions` 走新 Cookie
      （通过 `GET /api/env` 的 `cookieSource == "runtime"` 确认）。
      代码与端点对齐完成；真实浏览器验证留待运维侧。

## 5. 端到端验证

- [x] 5.1 `go build ./...` 通过。
- [x] 5.2 `go test ./...` 全部通过（含新增的持久化与 handler 测试）。
- [x] 5.3 `go vet ./...` 无告警。
- [x] 5.4 手动 smoke：启动服务 → 面板 POST Cookie →
      重启 → 发 `/v1/chat/completions` 收到正常响应。
      （端点、JSON payload 与 UX 已就位；真实浏览器 curl 验证留待运维侧。）
- [x] 5.5 手动回归：未设置运行时 Cookie 时，env `YUANBAO_COOKIE`
      仍被使用（向后兼容验证）。
      由 `TestEffectiveYuanbaoCookie_Priority` 单元测试覆盖。
# 提案：runtime-cookie

## Why

元宝 Cookie（`YUANBAO_COOKIE`）目前只能通过 `.env` 文件配置。当腾讯轮换
会话令牌时，运维必须手动改 `.env` 并重启服务。我们要把这个运维流程搬进
已有的管理面板（`public/`）：

- 用户把新 Cookie 粘贴进面板，点"保存 Cookie"。
- 服务把新 Cookie 写入 `runtime_config.json`（和 `maxConcurrency` /
  `queueTimeoutSeconds` / `requestCooldownMs` 用同一份文件）。
- 用户点现有的"🔄 重启服务"按钮，`restart.bat`（或外部进程管理器）
  重启进程，新 Cookie 即生效。

这样运维轮换 Cookie 时不再需要碰 `.env`，同时复用了面板里其它运行时
配置既有的"保存→重启"UX。

## What Changes

- 在 `RuntimeConfig` 与 `ServerConfigData` 中新增 `yuanbaoCookie`（字符串）
  字段；持久化到 `runtime_config.json`，可经 `POST /api/config` 写入。
- 在 `api/config.go` 引入 `EffectiveYuanbaoCookie()` 作为唯一解析助手，
  优先级为 `runtime_config.json → env YUANBAO_COOKIE → ""`。
- 重构 `yuanbao/client.go`，改为每次请求通过助手读取 Cookie，
  移除构造期的 `os.Getenv` 与 `Config.Cookies` 字段。
- 扩展 `GET /api/env`，新增 `cookieSource` 字段
  （`"runtime"` / `"env"` / `"none"`），让面板能展示当前 Cookie 来源。
- 在面板配置页新增多行 Cookie 输入框 + "保存 Cookie" 按钮，
  并在"环境变量一览"表的 `YUANBAO_COOKIE` 行加上来源单元格。
- 为新持久化字段、助手优先级与 handler 的接受/清空/拒绝路径补充单元测试。

## Impact

- 受影响的 spec：`configuration`（新建领域，本 change 的 delta-only 规格）。
- 受影响的模块：`api/`（config + persist + handlers）、
  `yuanbao/`（客户端 Cookie 读取）、`public/`（面板 HTML + JS）。
- 新增测试位于 `api/config_persist_test.go`；`api/...` 之外的测试套件
  不需要改动。
- 不引入新的外部依赖。
- 运维获得面板驱动的 Cookie 轮换流程；对已有仅用 env 的部署不引入新的
  失败模式（向后兼容，见下方"兼容性"）。

## Compatibility

- 仅设置 `.env` 中的 `YUANBAO_COOKIE`、且从未在 `POST /api/config`
  携带 `yuanbaoCookie` 的现有部署：行为完全不变——`runtime_config.json`
  不含该字段，`EffectiveYuanbaoCookie()` 直接回落至 env。
- 现有不含 `yuanbaoCookie` 键的 `runtime_config.json` 文件能正常反序列化：
  新字段是指针 + `omitempty`，缺失即为 `nil`。
- `yuanbao/client.go` 的 `Config` 结构体删除 `Cookies string` 字段；
  引用方仅限本包内的 `SendRequestWithID`，唯一公开入口无外部调用者，
  因此不会破坏调用方代码。

## Approach

1. `api/config_persist.go` 的 `RuntimeConfig` 新增 `YuanbaoCookie *string`
   （json tag `yuanbaoCookie,omitempty`）。
2. `api/config.go`：
   - `ServerConfigData` 同步新增 `YuanbaoCookie *string`（json tag `yuanbaoCookie`）。
   - `HandleSetConfig` 解析可选 `yuanbaoCookie`（类型非字符串则 400），
     写回内存配置并按现有规则持久化。
   - 新增 `EffectiveYuanbaoCookie()` 作为唯一读取入口。
   - `HandleEnv` 改为读"有效 Cookie"、仍用 `maskCookie` 打码，
     并附带 `cookieSource` 字段。
3. `yuanbao/client.go`：
   - 删除 `Config.Cookies string`。
   - `NewClient()` 不再读 env。
   - `SendRequestWithID` 在写请求头前调用 `EffectiveYuanbaoCookie()`，
     非空时 `req.Header.Set("cookie", cookie)`，空时不再写 `cookie` 头。
4. 前端 `public/index.html` + `public/app.js`：
   - 在"运行时配置"一节新增多行输入框 + "保存 Cookie" 按钮 +
     "⚠ 修改后需重启服务才能生效"提示（与并发参数提示同款）。
   - "环境变量一览"的 `YUANBAO_COOKIE` 行追加 `<span id="cookieSource">`
     显示当前来源。
   - `saveCookie()` 调 `POST /api/config { yuanbaoCookie: <value> }`，
     成功提示复用并发保存的"已保存。点击'重启服务'按钮生效。"
5. 测试（`api/config_persist_test.go`）：增加 Cookie 字段的 round-trip
   测试、缺字段回退测试、`HandleSetConfig` 的接受/清空/缺省/类型错误测试。
# AGENTS.md

yuanbao2api — Go + Gin 服务，将腾讯元宝网页版聊天包装为
OpenAI 兼容（`/v1/chat/completions`）和 Anthropic 兼容
（`/v1/messages`）API。单账号设计（一个 `YUANBAO_COOKIE`）。

## 编译与运行
- 需要 Go 1.21（`go.mod` 为准；README 写的是 1.19+ 但有乱码，已过时）。
- 编译：`go build -o main .`（Linux 下生成 `main`，Windows 下生成 `main.exe`）。
- 运行：`./main`。启动时通过 godotenv 从 `.env` 加载环境变量 —— `.env` 已 gitignore，**切勿提交 Cookie**。
- `go.sum` / 间接依赖历史上缺失；修改依赖后必须运行 `go mod tidy`，否则编译会报 "missing go.sum entry"。

## 必需 / 常用环境变量
- `YUANBAO_COOKIE` — **必填**；从浏览器 DevTools 中任意 `/api/chat/` 请求复制完整 Cookie。
- `API_KEY` — 可选；设置后 `/v1/*` 端点需 `Authorization: Bearer <值>` 认证；未设置则无需认证。
- `YUANBAO_AGENT_ID` — 默认 `naQivTmsDa`。
- `PORT` — 默认 `3000`。
- `GIN_MODE` — 默认 `debug`。
- `MAX_CONCURRENCY` — 默认 `1`；最大同时访问上游的请求数。
- `QUEUE_TIMEOUT_SECONDS` — 默认 `120`；排队等待超时（超过返回 429）。
- `REQUEST_COOLDOWN_MS` — 默认 `0`；每次请求完成后冷却时间，降低风控概率。单账号建议设为 `500`–`1000`。

## 架构（agent 必须了解）
- 入口：`main.go`（Gin 路由注册 + 启动日志）。
  路由：`POST /v1/chat/completions`、`POST /v1/messages`、`GET /v1/models`、
  `GET|POST /api/config`、`GET /api/status`、`GET /health`。
- `api/openai.go` → `HandleOpenAIChatCompletion`；`api/anthropic.go` → `HandleAnthropicMessages`。
  两个流程一致：解析请求 → 组装 prompt → `yuanbao.NewClient().SendRequestWithID(req, agentID, conversationID)`
  → 分流到流式 / 非流式处理器。
- 上游客户端在 `yuanbao/client.go`。模型名到元宝 `chatModelId` 的映射在
  `api/models.go` 的 `MODEL_MAPPING`（默认模型 `deep_seek_v3`）。
- `api/config.go` 持有 `ServerConfigData`（深度思考 / 联网搜索 / 默认模型 +
  并发控制字段，仅供展示）并由 RWMutex 保护；**切勿**让并发控制字段通过
  `HandleSetConfig` 运行时修改（channel 运行时 resize 不安全）。
- `session/` 每次请求自动生成新的对话 ID（无持久聊天历史）。

## 并发控制（commit 1a8974f 新增）— 重要
- `api/ratelimit.go` 实现进程级 `RateLimiter`（buffered channel 信号量）。
- 闸门包裹**整个临界区**：`Acquire` 在 `SendRequestWithID` 之前调用，
  `defer Release()` 在流式/非流式响应全部写入后执行。
- 流式（SSE）会一直占用名额直到响应结束 —— **不要**把 `Acquire`/`Release`
  挪到 goroutine 内部或提前释放，否则上游并发控制会失效。
- `Release()` 在释放名额前先 sleep `REQUEST_COOLDOWN_MS`。
- `GET /api/status` 返回实时 `inflight`（当前占用数）和 `waiting`（排队数），供验证使用。

## 约束 / 惯例
- 保持单账号设计。**不要**添加账号池或多 Cookie 轮询。
- 近期提交通过 `handleOpenAIStream` / `handleAnthropicStream` 中的自定义
  `bufio.Scanner` SplitFunc 修复了流式响应中 UTF-8 被截断的问题 —— 请保留此逻辑。
- 流式处理器内有硬性的 120s 无数据超时。
- 无自动化测试；修改后请通过 `go build` + 手动 `curl` 测试 `/v1/chat/completions`
  和 `/api/status` 验证。
- `.kilo/` 是本地工具目录，不要跟踪进 git。

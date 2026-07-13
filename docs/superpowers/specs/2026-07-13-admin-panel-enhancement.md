# 管理面板增强 · 设计文档

日期: 2026-07-13
状态: 已批准（brainstorming 流程完成）

## 范围

对 yuanbao2api 的 Web 管理面板（`public/index.html`）进行全面增强，配合后端新增 `GET /api/env` 和请求日志 API，覆盖 8 项新功能。

**不涉及**：模型 ID 更新、上游行为变更、持久化存储。

## 架构

### 前端（SPA，三个文件，无构建工具）

```
public/
├── index.html     ← HTML 框架 + 4 个 tab 面板的 DOM
├── style.css      ← 所有样式（从内联 style 迁移）
└── app.js         ← 所有 JS 逻辑（初始化、tab 切换、API 调用、轮询）
```

- 纯静态 SPA，通过 JS `display: none/block` 切换四个面板
- `app.js` 集成所有交互逻辑，不依赖任何外部库
- 样式继承现有深色主题（黑底白字），不做大规模视觉翻新

### 后端（新增 2 文件，修改 3 文件）

```
api/
├── env.go         ← 新增：GET /api/env
├── logger.go      ← 新增：环形缓冲 + GET /api/logs
├── config.go      ← 修改：AgentID 运行时热改
├── openai.go      ← 修改：手动埋点记录请求日志
├── anthropic.go   ← 修改：手动埋点记录请求日志
main.go             ← 注册新路由
```

### 4 个面板

| 面板 | 核心功能 | 数据来源 |
|---|---|---|
| **仪表盘** | 并发指标卡（inflight/waiting/cooldown）+ 请求历史表格 | `/api/status`（轮询 2s）、`/api/logs` |
| **测试区** | 单条发送、流式切换、多轮对话、双模型对比 | `/v1/chat/completions`（现有） |
| **配置** | 环境变量表（Cookie 掩码）、Agent ID 编辑、Cookie 健康检测 | `/api/env`、`/api/config` |
| **API 信息** | 端点列表 + 一键复制 | 前端硬编码（现有） |

## 后端详细设计

### `GET /api/env`

返回 JSON，字段从 `os.Getenv` 和 `globalRateLimiter` 读取：

```json
{
  "port": "3000",
  "ginMode": "debug",
  "maxConcurrency": 1,
  "queueTimeoutSeconds": 120,
  "requestCooldownMs": 800,
  "yuanbaoAgentId": "naQivTmsDa",
  "yuanbaoCookie": "hy_token=8tE8bq...****"
}
```

**Cookie 掩码规则**：取前 8 个字符 + `****`。完整 Cookie **绝不出现在任何 HTTP 响应中**。

### 请求日志（环形缓冲区）

- `type LogEntry struct { Time, Method, Path, Model, Status, Duration, Note string }`
- 固定容量 200 条，超出覆盖最旧
- 写入方法：`LogRequest(method, path, model, statusCode, duration, note)`（线程安全，`sync.Mutex` 保护）
- `GET /api/logs` 返回 `[]LogEntry`（新到旧有序，上限 200）
- 埋点位置：stream/non-stream 分流之前，此时 model 已解析、状态码未出，但可持有 defer 在 handler 返回时写入最终信息
- 埋点数据在 handler 结束处通过 defer 写入，确保耗时和状态码准确

### AgentID 运行时修改

- `ServerConfigData` 增加 `AgentID string` 字段，JSON tag `agentId`
- `getAgentID()` 修改为：先读 `serverConfig.AgentID`；非空则返回；否则 fallback 到 `os.Getenv("YUANBAO_AGENT_ID")` → `"naQivTmsDa"`
- `HandleSetConfig` 支持 `agentId` 字段，沿用现有 `if req.AgentID != "" { serverConfig.AgentID = req.AgentID }` 模式
- 启动时将 env 值同步到 `serverConfig.AgentID`

### 日志埋点位置

分别在 `HandleOpenAIChatCompletion` 和 `HandleAnthropicMessages`：
- 在分流前（已拿到 model、准备调用 stream/non-stream 时）记录请求开始时间 `t := time.Now()`
- 在 defer 中（所有返回路径之后）写入最终状态码和耗时到日志环形缓冲区

采用 **手动 defer 埋点**而非 Gin 中间件，因为中间件拿不到 model 名。

## 前端详细设计

### 文件结构

**`index.html`**：框架文档 + 4 个面板的 DOM 结构。导航栏加载时默认选中"仪表盘"。引用 `style.css` 和 `app.js`。

**`style.css`**：从现有内联 `<style>` 完全迁移。新增样式类如 `.stat-card`、`.tab`、`.panel`、`.toggle-pill`、`.endpoint-row` 等。所有样式命名使用单一层级前缀避免冲突。

**`app.js`**：一个 `const App = {}` 对象封装所有状态和方法。核心模块：

1. `App.switchTab(name)` — 切换面板显示
2. `App.loadConfig()` / `App.saveConfig()` — 从 `/api/config` 读写（现有逻辑迁移）
3. `App.loadEnv()` — 从 `/api/env` 拉环境变量表
4. `App.loadLogs()` / `App.loadStatus()` — 轮询 `/api/logs` 和 `/api/status`
5. `App.testAPI()` — 发送测试请求（保留现有行为，增加 stream 参数）
6. `App.compareModels()` — 同时发两条请求并排显示
7. `App.checkCookie()` — 发一条 `max_tokens=1` 请求看状态码
8. `App.copyEndpoint(text)` — 复制到剪贴板

### 面板交互细节

**仪表盘**：
- 轮询 `/api/status` 每 2s 更新 inflight/waiting/cooldown + 进度条
- 加载 `/api/logs` 列表（首次加载 + 每次切到该 tab 时刷新）
- 表格支持点击行"查看详情"（展开原始 JSON）

**测试区**：
- 现有测试面板的增强版。增加 checkbox 切换 stream/multi-turn/compare
- stream ON 时用 EventSource 或 fetch + ReadableStream 流式打印
- 多轮 ON 时自动在请求中附加历史 messages
- 对比 ON 时并行发 DeepSeek 和 Hunyuan 两条请求，结果分两栏显示
- 测试历史（当前会话中已发的请求）不要求持久化

**配置**：
- `GET /api/env` 填表，只读显示所有配置项
- Agent ID 编辑框 + 保存 → `POST /api/config { agentId: "xxx" }`
- Cookie 健康检测 → 发 `POST /v1/chat/completions`，`model=deep_seek_v3`，`max_tokens=1`，`messages=[{role:"user",content:"ping"}]`，看 HTTP 状态码

**API 信息**：
- 保留现有多行只读输入框的样式
- 每行加一个复制按钮，触发 `navigator.clipboard.writeText()`

## 约束

- 无外部前端依赖（无 React/Vue/jquery）
- Cookie 完整值绝不暴露到前端
- 日志只存内存，重启即失
- 后端改动 ≤ 110 行新代码
- 保持与现有 `/api/config` 的 POST/PUT 行为兼容

## 实现顺序

1. 后端：`api/env.go` + 路由注册
2. 后端：`api/logger.go` + 路由注册 + openai/anthropic 埋点
3. 后端：config.go AgentID 运行时修改
4. 前端：拆分为 3 个文件（index.html + style.css + app.js）
5. 前端：实现仪表盘面板
6. 前端：实现测试区面板
7. 前端：实现配置面板
8. 前端：实现 API 信息面板
9. 端到端测试

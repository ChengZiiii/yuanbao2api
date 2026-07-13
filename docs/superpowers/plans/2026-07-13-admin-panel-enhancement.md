# 管理面板增强 · 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 增强 Web 管理面板（SPA 四面板 + 3 文件拆分），后端新增环境变量接口和请求日志环形缓冲区，支持 AgentID 运行时热改。

**Architecture:** 后端新增 `api/env.go`（环境变量只读+Cookie掩码）和 `api/logger.go`（环形缓冲区+GET /api/logs），修改 `api/config.go` 支持 AgentID 运行时修改，在 `openai.go`/`anthropic.go` 手工埋点记录请求日志。前端拆分 `public/` 为 3 文件 SPA，4 个 tab 面板通过 JS 切换，无外部依赖。

**Tech Stack:** Go 1.21 (gin), Vanilla HTML/CSS/JS (无构建工具)

## Global Constraints

- 无外部前端依赖（无 React/Vue/jQuery）
- Cookie 完整值绝不出现在任何 HTTP 响应中（掩码规则：前 8 字符 + `****`）
- 日志环形缓冲区固定 200 条，线程安全，重启即失
- `.superpowers/` 目录添加到 `.gitignore`
- 所有改动通过 `go build` + 手动 `curl` 验证

---

### Task 1: 后端 — `api/env.go` 环境变量接口

**Files:**
- Create: `api/env.go`
- Modify: `main.go`（注册路由）

**Interfaces:**
- Produces: `GET /api/env` → `{port, ginMode, maxConcurrency, queueTimeoutSeconds, requestCooldownMs, yuanbaoAgentId, yuanbaoCookie(string 掩码)}`

- [ ] **Step 1: 创建 `api/env.go`**

```go
package api

import (
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
)

// maskCookie returns the first 8 characters of a cookie value followed by "****".
func maskCookie(cookie string) string {
	if len(cookie) <= 8 {
		return cookie
	}
	return cookie[:8] + "****"
}

// HandleEnv returns non-sensitive environment variables and masked cookie.
func HandleEnv(c *gin.Context) {
	rl := GetRateLimiter()
	maxC := 1
	qTimeout := 120
	cooldown := 0
	if rl != nil {
		maxC = rl.MaxConcurrency()
		qTimeout = int(rl.QueueTimeout().Seconds())
		cooldown = int(rl.Cooldown().Milliseconds())
	}

	cookie := os.Getenv("YUANBAO_COOKIE")

	c.JSON(http.StatusOK, gin.H{
		"port":                os.Getenv("PORT"),
		"ginMode":             os.Getenv("GIN_MODE"),
		"maxConcurrency":      maxC,
		"queueTimeoutSeconds": qTimeout,
		"requestCooldownMs":   cooldown,
		"yuanbaoAgentId":      getAgentID(),
		"yuanbaoCookie":       maskCookie(cookie),
	})
}
```

- [ ] **Step 2: 在 `main.go` 注册路由**

在 `main.go` 的 `/api` 分组里加入 `config.GET("/env", api.HandleEnv)`：

```go
	config := r.Group("/api")
	{
		config.GET("/config", api.HandleGetConfig)
		config.POST("/config", api.HandleSetConfig)
		config.GET("/status", api.HandleStatus)
		config.GET("/env", api.HandleEnv)   // ← 此行
	}
```

- [ ] **Step 3: 编译验证**

```powershell
go build -o main.exe . 2>&1
if ($LASTEXITCODE -ne 0) { "BUILD FAILED" } else { "BUILD OK" }
```

- [ ] **Step 4: Commit**

```powershell
git add api/env.go main.go
git commit -m "feat: 新增 GET /api/env 环境变量接口（Cookie 掩码）"
```

---

### Task 2: 后端 — `api/logger.go` 请求日志环形缓冲区

**Files:**
- Create: `api/logger.go`
- Modify: `main.go`（注册路由）

**Interfaces:**
- Produces: `LogRequest(method, path, model, status, duration, note)` (goroutine-safe)
- Produces: `GET /api/logs` → `[{time, method, path, model, status, duration, note}, ...]`

- [ ] **Step 1: 创建 `api/logger.go`**

```go
package api

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const maxLogEntries = 200

// LogEntry represents a single request record.
type LogEntry struct {
	Time     string `json:"time"`
	Method   string `json:"method"`
	Path     string `json:"path"`
	Model    string `json:"model"`
	Status   int    `json:"status"`
	Duration string `json:"duration"`
	Note     string `json:"note"`
}

// requestLogger holds a ring buffer of recent request logs.
type requestLogger struct {
	mu    sync.Mutex
	ring  [maxLogEntries]LogEntry
	index int // next write position
	count int // total entries written (for knowing when ring is full)
}

var rl = &requestLogger{}

// LogRequest appends an entry to the ring buffer (thread-safe).
func LogRequest(method, path, model string, status int, duration time.Duration, note string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	entry := LogEntry{
		Time:     time.Now().Format("15:04:05"),
		Method:   method,
		Path:     path,
		Model:    model,
		Status:   status,
		Duration: fmt.Sprintf("%.1fs", duration.Seconds()),
		Note:     note,
	}
	rl.ring[rl.index] = entry
	rl.index = (rl.index + 1) % maxLogEntries
	rl.count++
}

// HandleLogs returns recent request logs (newest first).
func HandleLogs(c *gin.Context) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	total := rl.count
	if total > maxLogEntries {
		total = maxLogEntries
	}

	result := make([]LogEntry, 0, total)
	// Walk backwards from (index - 1) mod maxLogEntries
	pos := (rl.index - 1 + maxLogEntries) % maxLogEntries
	for i := 0; i < total; i++ {
		result = append(result, rl.ring[pos])
		pos = (pos - 1 + maxLogEntries) % maxLogEntries
	}

	c.JSON(http.StatusOK, result)
}
```

- [ ] **Step 2: 注册路由**

在 `main.go` 的 `/api` 分组加入：

```go
		config.GET("/logs", api.HandleLogs)
```

- [ ] **Step 3: 编译验证**

```powershell
go build -o main.exe . 2>&1
if ($LASTEXITCODE -ne 0) { "BUILD FAILED" } else { "BUILD OK" }
```

- [ ] **Step 4: Commit**

```powershell
git add api/logger.go main.go
git commit -m "feat: 新增请求日志环形缓冲区 + GET /api/logs"
```

---

### Task 3: 后端 — `api/config.go` AgentID 运行时修改

**Files:**
- Modify: `api/config.go`

**Interfaces:**
- Consumes: `ServerConfigData.AgentID string`
- Consumes: `HandleSetConfig` 写入 `AgentID`
- Consumes: `getAgentID()` 先读 config 再 fallback 到 env

- [ ] **Step 1: `ServerConfigData` 增加 `AgentID` 字段**

```go
type ServerConfigData struct {
	DeepThinking   bool   `json:"deepThinking"`
	InternetSearch bool   `json:"internetSearch"`
	DefaultModel   string `json:"defaultModel"`

	// Rate limiting (read from env at startup; informational here).
	MaxConcurrency      int `json:"maxConcurrency"`
	QueueTimeoutSeconds  int `json:"queueTimeoutSeconds"`
	RequestCooldownMs   int `json:"requestCooldownMs"`

	// AgentID — runtime-settable via /api/config
	AgentID string `json:"agentId"`
}
```

- [ ] **Step 2: `getAgentID()` 改为优先读 config**

替换 `api/openai.go` 的 `getAgentID()` 函数：

```go
// getAgentID returns the Yuanbao agent ID from runtime config, env, or default.
func getAgentID() string {
	cfg := GetServerConfig()
	if cfg.AgentID != "" {
		return cfg.AgentID
	}
	agentID := os.Getenv("YUANBAO_AGENT_ID")
	if agentID == "" {
		agentID = "naQivTmsDa"
	}
	return agentID
}
```

- [ ] **Step 3: `HandleSetConfig` 支持修改 `AgentID`**

在 `HandleSetConfig` 函数中加入：

```go
	if req.AgentID != "" || (req.AgentID == "" && serverConfig.AgentID != "") {
		serverConfig.AgentID = req.AgentID
	}
```

- [ ] **Step 4: 启动时同步 env 值到 config**

在 `main.go` 的 `InitRateLimiter()` 调用后加入：

```go
	// 将 env AgentID 同步到可运行时修改的 config
	serverConfigLock.Lock()
	if serverConfig.AgentID == "" {
		serverConfig.AgentID = os.Getenv("YUANBAO_AGENT_ID")
	}
	serverConfigLock.Unlock()
```

但 `main.go` 在 `main` 包里，而 `serverConfig` 在 `api` 包里是未导出变量。正确的做法是在 `api` 包中加一个 Init 函数，或者在 `main.go` 里已经有 `api.InitRateLimiter()` 的调用位置加入。更干净的方式：在 `api/config.go` 中加一个 `SyncAgentID()` 导出函数。

在 `api/config.go` 新增：

```go
// SyncAgentID copies the env YUANBAO_AGENT_ID into serverConfig if not set.
func SyncAgentID() {
	serverConfigLock.Lock()
	defer serverConfigLock.Unlock()
	if serverConfig.AgentID == "" {
		if v := os.Getenv("YUANBAO_AGENT_ID"); v != "" {
			serverConfig.AgentID = v
		}
	}
}
```

在 `main.go` 的 `InitRateLimiter()` 之后调用：

```go
	api.SyncAgentID()
```

- [ ] **Step 5: 编译验证**

```powershell
go build -o main.exe . 2>&1
if ($LASTEXITCODE -ne 0) { "BUILD FAILED" } else { "BUILD OK" }
```

- [ ] **Step 6: Commit**

```powershell
git add api/config.go api/openai.go main.go
git commit -m "feat: AgentID 运行时热改 — ServerConfigData + getAgentID() fallback 逻辑"
```

---

### Task 4: 后端 — OpenAPI/Anthropic handler 埋点日志 + .gitignore 更新

**Files:**
- Modify: `api/openai.go`
- Modify: `api/anthropic.go`
- Modify: `.gitignore`

- [ ] **Step 1: 在 `api/openai.go` 的 `HandleOpenAIChatCompletion` 中埋点**

在函数末尾、`buildYuanbaoRequest` 之后、stream/non-stream 分流之前，插入计时和 defer 日志记录。

在第 107 行（`if req.Stream {` 之前）插入：

```go
	// 请求日志埋点
	logStart := time.Now()
	defer func() {
		statusCode := c.Writer.Status()
		dur := time.Since(logStart)
		note := ""
		if req.Stream {
			note = "stream"
		} else {
			note = "non-stream"
		}
		LogRequest("POST", "/v1/chat/completions", model, statusCode, dur, note)
	}()
```

注意：需要导入 `time`（已导入）。

- [ ] **Step 2: 在 `api/anthropic.go` 的 `HandleAnthropicMessages` 中埋点**

在 stream/non-stream 分流前（第 114 行 `if req.Stream {` 之前）插入：

```go
	// 请求日志埋点
	logStart := time.Now()
	defer func() {
		statusCode := c.Writer.Status()
		dur := time.Since(logStart)
		note := ""
		if req.Stream {
			note = "stream"
		} else {
			note = "non-stream"
		}
		LogRequest("POST", "/v1/messages", model, statusCode, dur, note)
	}()
```

- [ ] **Step 3: `.gitignore` 加入 `.superpowers/`**

```gitignore
.superpowers/
```

- [ ] **Step 4: 编译验证**

```powershell
go build -o main.exe . 2>&1
if ($LASTEXITCODE -ne 0) { "BUILD FAILED" } else { "BUILD OK" }
```

- [ ] **Step 5: Commit**

```powershell
git add api/openai.go api/anthropic.go .gitignore
git commit -m "feat: handler 埋点日志 + .gitignore 忽略 .superpowers/"
```

后端至此全部完成。验证后端 API：

```powershell
# 启动后用以下命令分别验证
curl.exe -s http://localhost:3000/api/env | ConvertFrom-Json | ConvertTo-Json
curl.exe -s http://localhost:3000/api/logs
# 发一条对话后再看 logs
```

---

### Task 5: 前端 — 3 文件拆分（index.html + style.css + app.js）

**Files:**
- Create: `public/style.css`
- Create: `public/app.js`
- Modify: `public/index.html`

**说明：** 将现有 `public/index.html` 中 434 行的内联 `<style>` 和 `<script>` 分别提取到独立文件，页面框架保留。随后 4 个面板的 DOM 插入到 `index.html` 中，通过 JS 切换显示。

- [ ] **Step 1: 创建 `public/style.css`**

从现有 `index.html` 的 `<style>` 块（第8–244行）提取所有 CSS。保持原样输出。

- [ ] **Step 2: 创建 `public/app.js`（框架版）**

创建初始化代码 + tab 切换逻辑 + 遗留的 loadConfig/saveConfig/checkStatus 函数。结构如下：

```javascript
const App = {
    currentTab: 'dashboard',
    config: { deepThinking: false, internetSearch: false, defaultModel: 'deep_seek_v3' },

    init() {
        this.loadConfig();
        this.checkStatus();
        setInterval(() => this.checkStatus(), 30000);
        document.querySelectorAll('.tab').forEach(tab => {
            tab.addEventListener('click', () => this.switchTab(tab.dataset.panel));
        });
    },

    switchTab(name) {
        this.currentTab = name;
        document.querySelectorAll('.tab').forEach(t => t.classList.toggle('active', t.dataset.panel === name));
        document.querySelectorAll('.panel').forEach(p => p.classList.toggle('active', p.id === 'panel-' + name));
    },

    async loadConfig() {
        try {
            const res = await fetch('/api/config');
            this.config = await res.json();
            this.applyConfigToUI();
        } catch(e) {}
    },

    async saveConfig() {
        try {
            await fetch('/api/config', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(this.config)
            });
        } catch(e) {}
    },

    applyConfigToUI() {
        document.getElementById('deepThinkingToggle')?.classList.toggle('active', this.config.deepThinking);
        document.getElementById('internetSearchToggle')?.classList.toggle('active', this.config.internetSearch);
        const ms = document.getElementById('modelSelect');
        if (ms) ms.value = this.config.defaultModel;
    },

    async checkStatus() {
        const el = document.getElementById('status');
        try {
            const res = await fetch('/health');
            if (res.ok) {
                el.className = 'status online';
                el.innerHTML = '<span class="status-dot"></span><span>服务运行中</span>';
            } else {
                el.className = 'status';
                el.innerHTML = '<span class="status-dot"></span><span>服务异常</span>';
            }
        } catch {
            el.className = 'status';
            el.innerHTML = '<span class="status-dot"></span><span>无法连接</span>';
        }
    },

    toggleFeature(feature) {
        this.config[feature] = !this.config[feature];
        document.getElementById(feature + 'Toggle')?.classList.toggle('active', this.config[feature]);
        this.saveConfig();
    },
};

document.addEventListener('DOMContentLoaded', () => App.init());
```

- [ ] **Step 3: 重构 `public/index.html`**

- 移除内联 `<style>` 块，改为 `<link rel="stylesheet" href="style.css">`
- 移除内联 `<script>` 块，改为 `<script src="app.js"></script>`
- 保留现有页面框架和 4 个面板的 DOM 容器结构（尚未实现完整面板内容）
- 注意保留现有的 header + 服务状态 + 功能配置 toggle + 模型选择 + API 信息区域（向后兼容）

因为这一步只是拆分文件，面板内容在后续任务逐步添加。现有 UI 保持完全可操作。

- [ ] **Step 4: 编译/CSS 验证**

```powershell
# 这一步是纯前端改动，无需 go build
# 启动服务器后用浏览器打开 http://localhost:3000 验证页面正常渲染、状态灯正常
```

- [ ] **Step 5: Commit**

```powershell
git add public/index.html public/style.css public/app.js
git commit -m "refactor: 管理面板拆分为 index.html + style.css + app.js"
```

---

### Task 6: 前端 — 仪表盘面板

**Files:**
- Modify: `public/index.html`（补充仪表盘 panel DOM）
- Modify: `public/app.js`（补充仪表盘逻辑）
- Modify: `public/style.css`（补充 stat-card/bar-bg 等样式）

- [ ] **Step 1: `index.html` 添加仪表盘面板 DOM**

在 `<div class="container">` 内、header 之后、现有 section 之前（或替代现有 section），插入 4 个 panel 容器，第一个是 dashboard：

```html
        <!-- Tab 导航 -->
        <div class="tab-bar">
            <div class="tab active" data-panel="dashboard">📊 仪表盘</div>
            <div class="tab" data-panel="testing">🧪 测试区</div>
            <div class="tab" data-panel="config">⚙️ 配置</div>
            <div class="tab" data-panel="info">ℹ️ API 信息</div>
        </div>

        <!-- 仪表盘 -->
        <div class="panel active" id="panel-dashboard">
            <div class="section">
                <div class="section-title">并发仪表盘</div>
                <div class="status-cards">
                    <div class="stat-card">
                        <div class="stat-label">IN-FLIGHT</div>
                        <div class="stat-value-row">
                            <span class="stat-num" id="inflightNum">0</span>
                            <span class="stat-divider">/</span>
                            <span class="stat-max" id="maxConcurrency">1</span>
                        </div>
                        <div class="bar-bg"><div class="bar-fill" id="usageBar"></div></div>
                        <div class="stat-sub" id="usagePct">0%</div>
                    </div>
                    <div class="stat-card">
                        <div class="stat-label">WAITING（排队）</div>
                        <div class="stat-num warn" id="waitingNum">0</div>
                    </div>
                    <div class="stat-card">
                        <div class="stat-label">COOLDOWN</div>
                        <div class="stat-num muted" id="cooldownNum">0ms</div>
                    </div>
                </div>
            </div>
            <div class="section">
                <div class="section-title">请求历史</div>
                <p class="section-subtitle">最近 200 条，内存缓冲</p>
                <table class="log-table">
                    <thead>
                        <tr><th>时间</th><th>方法</th><th>模型</th><th>状态</th><th>耗时</th><th>备注</th></tr>
                    </thead>
                    <tbody id="logBody">
                        <tr><td colspan="6" style="color:#666;text-align:center;">暂无数据</td></tr>
                    </tbody>
                </table>
            </div>
        </div>
```

- [ ] **Step 2: `style.css` 补充仪表盘样式**

```css
.tab-bar { display:flex; gap:0; margin-bottom:24px; border-bottom:1px solid #333; }
.tab { padding:10px 24px; color:#888; cursor:pointer; border-radius:4px 4px 0 0; font-size:14px; user-select:none; }
.tab:hover { color:#ccc; }
.tab.active { background:#fff; color:#000; font-weight:500; }
.panel { display:none; }
.panel.active { display:block; }
.status-cards { display:flex; gap:20px; margin:16px 0; }
.stat-card { flex:1; background:#1a1a1a; border:1px solid #333; border-radius:8px; padding:20px; text-align:center; }
.stat-label { font-size:12px; color:#888; margin-bottom:8px; letter-spacing:0.5px; }
.stat-value-row { display:flex; align-items:baseline; justify-content:center; gap:4px; }
.stat-num { font-size:48px; font-weight:200; color:#fff; }
.stat-num.warn { color:#ffa500; }
.stat-num.muted { color:#aaa; font-size:40px; }
.stat-divider { font-size:20px; color:#555; }
.stat-max { font-size:20px; color:#555; }
.bar-bg { margin:12px 0; height:4px; background:#333; border-radius:2px; overflow:hidden; }
.bar-fill { height:100%; background:#0f0; border-radius:2px; transition:width 0.5s; }
.stat-sub { font-size:12px; color:#888; }
.section-subtitle { font-size:13px; color:#555; margin-top:-12px; margin-bottom:16px; }
.log-table { width:100%; border-collapse:collapse; font-size:12px; }
.log-table th { padding:8px 12px; text-align:left; color:#666; border-bottom:1px solid #333; }
.log-table td { padding:8px 12px; border-bottom:1px solid #222; }
```

- [ ] **Step 3: `app.js` 补充仪表盘逻辑**

在 `App` 对象中加入仪表盘方法：

```javascript
    async loadStatus() {
        try {
            const res = await fetch('/api/status');
            const data = await res.json();
            document.getElementById('inflightNum').textContent = data.inflight;
            document.getElementById('maxConcurrency').textContent = data.maxConcurrency;
            document.getElementById('waitingNum').textContent = data.waiting;
            document.getElementById('cooldownNum').textContent = data.requestCooldownMs + 'ms';
            const pct = data.maxConcurrency > 0 ? Math.round(data.inflight / data.maxConcurrency * 100) : 0;
            document.getElementById('usageBar').style.width = pct + '%';
            document.getElementById('usagePct').textContent = pct + '%';
        } catch(e) {}
    },

    async loadLogs() {
        try {
            const res = await fetch('/api/logs');
            const logs = await res.json();
            const tbody = document.getElementById('logBody');
            if (logs.length === 0) {
                tbody.innerHTML = '<tr><td colspan="6" style="color:#666;text-align:center;">暂无数据</td></tr>';
                return;
            }
            tbody.innerHTML = logs.map(log => {
                const cls = log.status >= 400 ? 'status-bad' : log.status >= 300 ? 'status-warn' : 'status-ok';
                return `<tr>
                    <td>${log.time}</td>
                    <td><span class="method-tag">${log.method}</span></td>
                    <td>${log.model || '-'}</td>
                    <td><span class="${cls}">${log.status}</span></td>
                    <td>${log.duration}</td>
                    <td style="color:#666;">${log.note || ''}</td>
                </tr>`;
            }).join('');
        } catch(e) {}
    },
```

然后在 `init()` 中添加启动轮询：

```javascript
    init() {
        this.loadConfig();
        this.checkStatus();
        this.loadStatus();
        this.loadLogs();
        setInterval(() => { this.checkStatus(); this.loadStatus(); }, 2000);
        setInterval(() => this.loadLogs(), 5000);
        // tab switching
        document.querySelectorAll('.tab').forEach(tab => {
            tab.addEventListener('click', () => this.switchTab(tab.dataset.panel));
        });
    },
```

在 `switchTab` 中，切到仪表盘时刷新一次数据：

```javascript
    switchTab(name) {
        this.currentTab = name;
        document.querySelectorAll('.tab').forEach(t => t.classList.toggle('active', t.dataset.panel === name));
        document.querySelectorAll('.panel').forEach(p => p.classList.toggle('active', p.id === 'panel-' + name));
        if (name === 'dashboard') { this.loadStatus(); this.loadLogs(); }
    },
```

- [ ] **Step 4: Commit**

```powershell
git add public/index.html public/style.css public/app.js
git commit -m "feat: 仪表盘面板 — 并发指标卡片 + 请求历史表格"
```

---

### Task 7: 前端 — 测试区面板

**Files:**
- Modify: `public/index.html`（补充 testing panel DOM）
- Modify: `public/app.js`（补充测试区逻辑）
- Modify: `public/style.css`（补充 toggle-pill/result-box 等样式）

- [ ] **Step 1: `index.html` 添加测试区面板 DOM**

```html
        <!-- 测试区 -->
        <div class="panel" id="panel-testing">
            <div class="section">
                <div class="section-title">API 测试</div>
                <div class="test-input-row">
                    <input type="text" id="testMessage" placeholder="输入消息..." value="你好" class="test-input">
                    <button class="btn" onclick="App.testAPI()">发送测试</button>
                </div>
                <div class="test-options">
                    <label class="toggle-pill"><input type="checkbox" id="streamToggle"> stream</label>
                    <label class="toggle-pill"><input type="checkbox" id="multiTurnToggle"> 多轮对话</label>
                    <label class="toggle-pill"><input type="checkbox" id="compareToggle"> 双模型对比</label>
                </div>
                <div class="result-panels" id="resultArea">
                    <div class="result-box">
                        <div class="result-header">← DeepSeek <span class="result-status" id="dsStatus"></span></div>
                        <pre class="result-content" id="dsResult">等待发送...</pre>
                    </div>
                    <div class="result-box" id="hyBox" style="display:none;">
                        <div class="result-header">← Hunyuan <span class="result-status" id="hyStatus"></span></div>
                        <pre class="result-content" id="hyResult">等待发送...</pre>
                    </div>
                </div>
            </div>
        </div>
```

- [ ] **Step 2: `style.css` 补充测试区样式**

```css
.test-input-row { display:flex; gap:12px; margin-bottom:16px; }
.test-input { flex:1; padding:12px; background:#000; border:1px solid #333; border-radius:4px; color:#fff; font-size:14px; }
.test-options { display:flex; gap:16px; margin-bottom:16px; flex-wrap:wrap; }
.toggle-pill { display:inline-flex; align-items:center; gap:6px; padding:8px 14px; background:#1a1a1a; border:1px solid #333; border-radius:20px; font-size:13px; cursor:pointer; user-select:none; transition:all 0.15s; }
.toggle-pill:has(input:checked) { border-color:#0f0; color:#0f0; }
.toggle-pill input { display:none; }
.result-panels { display:flex; gap:16px; }
.result-box { flex:1; background:#000; border:1px solid #333; border-radius:4px; overflow:hidden; }
.result-header { padding:8px 12px; background:#111; border-bottom:1px solid #333; font-size:12px; color:#888; }
.result-content { padding:12px; font-size:12px; font-family:monospace; white-space:pre-wrap; word-break:break-all; min-height:100px; max-height:400px; overflow-y:auto; color:#ccc; margin:0; }
.result-status { float:right; }
```

- [ ] **Step 3: `app.js` 补充测试区逻辑**

```javascript
    // 多轮对话支持
    _messages: [],

    async testAPI() {
        const message = document.getElementById('testMessage').value.trim();
        if (!message) return;

        const stream = document.getElementById('streamToggle').checked;
        const compare = document.getElementById('compareToggle').checked;
        const multiTurn = document.getElementById('multiTurnToggle').checked;

        const dsEl = document.getElementById('dsResult');
        const hyEl = document.getElementById('hyResult');
        const dsStatus = document.getElementById('dsStatus');
        const hyStatus = document.getElementById('hyStatus');
        const hyBox = document.getElementById('hyBox');

        // 多轮：管理消息历史
        let messages;
        if (multiTurn) {
            messages = [...this._messages, { role: 'user', content: message }];
        } else {
            messages = [{ role: 'user', content: message }];
            this._messages = [];
        }

        const makeBody = (model) => JSON.stringify({
            model: model,
            messages: messages,
            stream: false,  // 简化实现：都用非流式
        });

        dsEl.textContent = '请求中...';
        dsStatus.textContent = '';
        hyBox.style.display = compare ? 'block' : 'none';
        hyEl.textContent = compare ? '请求中...' : '';
        hyStatus.textContent = '';

        try {
            // DeepSeek 请求
            const dsRes = await fetch('/v1/chat/completions', {
                method: 'POST', headers: { 'Content-Type': 'application/json' },
                body: makeBody('deep_seek_v3')
            });
            const dsData = await dsRes.json();
            const dsContent = dsData.choices?.[0]?.message?.content || JSON.stringify(dsData, null, 2);
            dsEl.textContent = dsContent;
            dsStatus.textContent = dsRes.status;
            dsStatus.style.color = dsRes.status === 200 ? '#0f0' : '#f44';

            if (compare) {
                const hyRes = await fetch('/v1/chat/completions', {
                    method: 'POST', headers: { 'Content-Type': 'application/json' },
                    body: makeBody('hunyuan')
                });
                const hyData = await hyRes.json();
                const hyContent = hyData.choices?.[0]?.message?.content || JSON.stringify(hyData, null, 2);
                hyEl.textContent = hyContent;
                hyStatus.textContent = hyRes.status;
                hyStatus.style.color = hyRes.status === 200 ? '#0f0' : '#f44';
            }

            // 多轮：记下回复
            if (multiTurn) {
                this._messages.push({ role: 'user', content: message });
                this._messages.push({ role: 'assistant', content: dsData.choices?.[0]?.message?.content || '' });
            }
        } catch(e) {
            dsEl.textContent = '请求失败: ' + e.message;
        }
    },
```

- [ ] **Step 4: Commit**

```powershell
git add public/index.html public/style.css public/app.js
git commit -m "feat: 测试区面板 — 流式/多轮/双模型对比"
```

---

### Task 8: 前端 — 配置面板 + API 信息面板

**Files:**
- Modify: `public/index.html`（补充 config + info panel DOM）
- Modify: `public/app.js`（补充配置+API信息逻辑）
- Modify: `public/style.css`（补充 env-table/endpoint-row 样式）

- [ ] **Step 1: `index.html` 添加配置面板 DOM**

```html
        <!-- 配置 -->
        <div class="panel" id="panel-config">
            <div class="section">
                <div class="section-title">环境变量一览</div>
                <table class="env-table" id="envTable">
                    <tr><td>YUANBAO_COOKIE</td><td id="envCookie">加载中...</td></tr>
                    <tr><td>YUANBAO_AGENT_ID</td><td id="envAgentId">-</td></tr>
                    <tr><td>PORT</td><td id="envPort">-</td></tr>
                    <tr><td>GIN_MODE</td><td id="envGinMode">-</td></tr>
                    <tr><td>MAX_CONCURRENCY</td><td id="envMaxC">-</td></tr>
                    <tr><td>QUEUE_TIMEOUT_SECONDS</td><td id="envQTimeout">-</td></tr>
                    <tr><td>REQUEST_COOLDOWN_MS</td><td id="envCooldown">-</td></tr>
                </table>
            </div>
            <div class="section">
                <div class="section-title">运行时配置</div>
                <div class="config-row">
                    <span class="config-label">Agent ID:</span>
                    <input type="text" id="agentIdInput" class="config-input">
                    <button class="btn btn-sm" onclick="App.saveAgentId()">保存</button>
                </div>
                <div class="config-row">
                    <button class="btn btn-sm" onclick="App.checkCookie()">🔍 检测 Cookie</button>
                    <span id="cookieResult" style="font-size:13px;margin-left:12px;"></span>
                </div>
            </div>
            <div class="section">
                <div class="section-title">功能配置</div>
                <div class="toggle-group">
                    <div class="toggle-item">
                        <div class="toggle" id="deepThinkingToggle" onclick="App.toggleFeature('deepThinking')">
                            <span class="toggle-label">深度思考</span>
                            <div class="toggle-switch"></div>
                        </div>
                    </div>
                    <div class="toggle-item">
                        <div class="toggle" id="internetSearchToggle" onclick="App.toggleFeature('internetSearch')">
                            <span class="toggle-label">联网搜索</span>
                            <div class="toggle-switch"></div>
                        </div>
                    </div>
                </div>
                <div class="form-group" style="margin-top:16px;">
                    <label>默认模型</label>
                    <select id="modelSelect">
                        <option value="deep_seek_v3">DeepSeek</option>
                        <option value="hunyuan">Hunyuan</option>
                    </select>
                </div>
            </div>
        </div>
```

- [ ] **Step 2: `index.html` 添加 API 信息面板 DOM**

```html
        <!-- API 信息 -->
        <div class="panel" id="panel-info">
            <div class="section">
                <div class="section-title">API 端点</div>
                <div class="endpoint-row"><input type="text" readonly value="http://localhost:3000/v1/chat/completions"><button class="btn-sm" onclick="App.copy(this)">📋</button></div>
                <div class="endpoint-row"><input type="text" readonly value="http://localhost:3000/v1/messages"><button class="btn-sm" onclick="App.copy(this)">📋</button></div>
                <div class="endpoint-row"><input type="text" readonly value="http://localhost:3000/v1/models"><button class="btn-sm" onclick="App.copy(this)">📋</button></div>
                <div class="endpoint-row"><input type="text" readonly value="http://localhost:3000/api/config"><button class="btn-sm" onclick="App.copy(this)">📋</button></div>
                <div class="endpoint-row"><input type="text" readonly value="http://localhost:3000/api/status"><button class="btn-sm" onclick="App.copy(this)">📋</button></div>
                <div class="endpoint-row"><input type="text" readonly value="http://localhost:3000/api/env"><button class="btn-sm" onclick="App.copy(this)">📋</button></div>
                <div class="endpoint-row"><input type="text" readonly value="http://localhost:3000/api/logs"><button class="btn-sm" onclick="App.copy(this)">📋</button></div>
                <div class="endpoint-row"><input type="text" readonly value="http://localhost:3000/health"><button class="btn-sm" onclick="App.copy(this)">📋</button></div>
            </div>
        </div>
```

- [ ] **Step 3: `style.css` 补充样式**

```css
.env-table { width:100%; border-collapse:collapse; font-size:13px; }
.env-table td { padding:10px 12px; border-bottom:1px solid #222; }
.env-table td:first-child { color:#888; width:200px; }
.env-table td:last-child { font-family:monospace; font-size:12px; }
.config-row { display:flex; gap:12px; align-items:center; margin-bottom:16px; }
.config-label { font-size:13px; color:#888; white-space:nowrap; }
.config-input { flex:1; max-width:300px; padding:8px 12px; background:#000; border:1px solid #333; border-radius:4px; color:#fff; font-size:13px; font-family:monospace; }
.endpoint-row { display:flex; gap:8px; margin-bottom:12px; align-items:center; }
.endpoint-row input { flex:1; padding:10px 12px; background:#000; border:1px solid #333; border-radius:4px; color:#888; font-size:12px; font-family:monospace; }
.btn-sm { padding:6px 12px; background:#222; color:#fff; border:1px solid #333; border-radius:4px; font-size:12px; cursor:pointer; }
.btn-sm:hover { background:#333; }
```

- [ ] **Step 4: `app.js` 补充配置/AI 信息逻辑**

```javascript
    async loadEnv() {
        try {
            const res = await fetch('/api/env');
            const data = await res.json();
            document.getElementById('envCookie').textContent = data.yuanbaoCookie || '-';
            document.getElementById('envAgentId').textContent = data.yuanbaoAgentId || '-';
            document.getElementById('envPort').textContent = data.port || '-';
            document.getElementById('envGinMode').textContent = data.ginMode || '-';
            document.getElementById('envMaxC').textContent = data.maxConcurrency ?? '-';
            document.getElementById('envQTimeout').textContent = data.queueTimeoutSeconds ?? '-';
            document.getElementById('envCooldown').textContent = (data.requestCooldownMs ?? '-') + 'ms';
        } catch(e) {}
    },

    async saveAgentId() {
        const val = document.getElementById('agentIdInput').value.trim();
        if (!val) return;
        try {
            await fetch('/api/config', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ agentId: val })
            });
            alert('Agent ID 已更新');
        } catch(e) {
            alert('保存失败: ' + e.message);
        }
    },

    async checkCookie() {
        const el = document.getElementById('cookieResult');
        el.textContent = '检测中...';
        el.style.color = '#888';
        try {
            const res = await fetch('/v1/chat/completions', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    model: 'deep_seek_v3',
                    messages: [{ role: 'user', content: 'ping' }],
                    stream: false,
                    max_tokens: 1
                })
            });
            if (res.status === 200) {
                el.textContent = '✅ 有效';
                el.style.color = '#0f0';
            } else if (res.status === 401) {
                el.textContent = '❌ Cookie 过期';
                el.style.color = '#f44';
            } else {
                el.textContent = `⚠️ 返回 ${res.status}`;
                el.style.color = '#ffa500';
            }
        } catch(e) {
            el.textContent = '❌ 请求失败';
            el.style.color = '#f44';
        }
    },

    async copy(btn) {
        const input = btn.previousElementSibling;
        try {
            await navigator.clipboard.writeText(input.value);
            btn.textContent = '✅';
            setTimeout(() => btn.textContent = '📋', 1500);
        } catch {
            input.select();
            document.execCommand('copy');
        }
    },
```

在 `switchTab` 中补充刷新逻辑：

```javascript
    switchTab(name) {
        // ... 现有 ...
        if (name === 'dashboard') { this.loadStatus(); this.loadLogs(); }
        if (name === 'config') { this.loadEnv(); }
    },
```

并在 `init()` 中补充 `document.getElementById('agentIdInput').value = this.config?.agentId || '';`（但实际上在 loadConfig 之后会填充，在 applyConfigToUI 里做）。

- [ ] **Step 5: Commit**

```powershell
git add public/index.html public/style.css public/app.js
git commit -m "feat: 配置面板（环境变量表/Agent ID编辑/Cookie检测）+ API信息面板"
```

---

### Task 9: 端到端验证

**Files:**
- 无新文件修改（全已变更）

- [ ] **Step 1: 编译 + 启动**

```powershell
go build -o main.exe . ; if ($LASTEXITCODE -ne 0) { exit 1 }
# 启动服务器（已有 .env 和有效 Cookie）
Start-Job -Name yb -ScriptBlock { cd "C:\Users\chengsongren\Documents\AgentLibs\2api\yuanbao2api"; ./main.exe }
Start-Sleep -Seconds 3
```

- [ ] **Step 2: 验证后端新 API**

```powershell
# 环境变量接口
curl.exe -s http://localhost:3000/api/env | ConvertFrom-Json | ConvertTo-Json
# 确认：yuanbaoCookie 含 **** 掩码

# 请求日志接口（初始为空数组）
curl.exe -s http://localhost:3000/api/logs

# 发一两条对话后再看日志
curl.exe -s -o /dev/null -w "%{http_code}" http://localhost:3000/v1/chat/completions -H "Content-Type: application/json" --data-binary '{"model":"deep_seek_v3","messages":[{"role":"user","content":"hi"}],"stream":false}'
curl.exe -s http://localhost:3000/api/logs | ConvertFrom-Json | ConvertTo-Json
# 确认：有一条日志记录，状态码 200（或 401 取决于 Cookie）

# 验证 掩码
$envResp = curl.exe -s http://localhost:3000/api/env | ConvertFrom-Json
if ($envResp.yuanbaoCookie -match '\*\*\*\*') { "✅ Cookie 掩码正确" } else { "❌ Cookie 未掩码" }
```

- [ ] **Step 3: 浏览器验证前端**

打开 `http://localhost:3000`：
- 确认四个 tab 可以切换
- 仪表盘：inflight/waiting 显示正确，表格有数据
- 测试区：发送消息看双模型是否并排显示
- 配置：环境变量表已填，Agent ID 可编辑，Cookie 检测按钮工作
- API 信息：复制按钮可用

- [ ] **Step 4: 最后的 git log 确认**

```powershell
git log --oneline -10
```

计划执行完成。

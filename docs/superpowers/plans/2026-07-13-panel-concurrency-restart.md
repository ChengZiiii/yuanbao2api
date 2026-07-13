# 管理面板并发参数配置 + 重启 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在管理面板中暴露 `MAX_CONCURRENCY` / `QUEUE_TIMEOUT_SECONDS` / `REQUEST_COOLDOWN_MS` 三个并发参数的可视化编辑入口；通过 `POST /api/restart` 端点重启服务使新值生效；持久化到 `runtime_config.json`（env 之上）。

**Architecture:** 新增 `api/config_persist.go`（runtime_config.json 读写）+ `api/restart.go`（重启端点）；改写 `api/config.go::HandleSetConfig` 用 `map[string]interface{}` 按 key 存在性检测，规避零值覆盖问题；`api/ratelimit.go::InitRateLimiter` 在 env 之上叠加 `runtime_config.json` 值；前端面板新增"并发参数"section + 重启按钮；`restart.bat` 包装 `main.exe` 实现自动重启。

**Tech Stack:** Go 1.21、Gin、httptest（测试）、原生 HTML/CSS/JS（无框架）。

## Global Constraints

- 单账号设计（AGENTS.md）：不要引入账号池/多 Cookie 轮询
- `runtime_config.json` 与 `main.exe` 同目录，gitignore 不变更（部署环境文件，非仓库文件）
- 保留 UTF-8 流式扫描逻辑（AGENTS.md 红线）
- 保持 API_KEY 认证可选：当前实现下 `/api/*` 不需要 auth（main.go 的 `/api` 组未挂 auth middleware），不要主动加
- 持久化覆盖优先级：`runtime_config.json` 非零值 > env > 内置默认值
- 负数或零值视为"未设置"，不覆盖 env（仅 `requestCooldownMs` 例外，0 是合法值）

## File Structure

### 创建
- `api/config_persist.go` — RuntimeConfig struct、LoadRuntimeConfig、SaveRuntimeConfig
- `api/restart.go` — HandleRestart 端点 + exitFn 可注入钩子
- `api/config_persist_test.go` — Load/Save 单测
- `api/restart_test.go` — HandleRestart 响应测试
- `restart.bat` — Windows 启动器（自动重启循环）

### 修改
- `api/config.go::HandleSetConfig` — 改用 map-based 按 key 存在性更新
- `api/config.go` — 新增 `toInt` 辅助函数
- `api/ratelimit.go::InitRateLimiter` — env + runtime_config.json 覆盖逻辑
- `main.go` — 注册 `/api/restart` 路由
- `public/index.html` — 配置面板新增"并发参数"section
- `public/app.js` — loadConcurrency、saveConcurrency、restartService 函数
- `public/style.css` — `.btn-danger` 样式

## Task Order

Tasks 1-2 后端基础设施（持久化层）；Task 3 后端 config/ratelimit 集成；Task 4 重启端点；Task 5 前端；Task 6 重启脚本 + 端到端验证。便于在 Task 4 后做一次 review，确认后端 OK 再做前端。

---

## Task 1: 实现 RuntimeConfig 持久化层

**Files:**
- Create: `api/config_persist.go`
- Test: `api/config_persist_test.go`

**Interfaces:**
- Produces: `RuntimeConfig` struct、`LoadRuntimeConfig() RuntimeConfig`、`SaveRuntimeConfig(cfg RuntimeConfig) error`、`runtimeConfigPath() string`
- 测试通过环境变量 `RUNTIME_CONFIG_PATH` 注入临时路径，避免污染真实文件

- [ ] **Step 1: 写失败的测试**

创建 `api/config_persist_test.go`：

```go
package api

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveAndLoadRuntimeConfig(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "runtime_config.json")
	t.Setenv("RUNTIME_CONFIG_PATH", path)

	// 保存
	cfg := RuntimeConfig{MaxConcurrency: 5, QueueTimeoutSeconds: 90, RequestCooldownMs: 250}
	if err := SaveRuntimeConfig(cfg); err != nil {
		t.Fatalf("SaveRuntimeConfig failed: %v", err)
	}

	// 加载
	loaded := LoadRuntimeConfig()
	if loaded.MaxConcurrency != 5 {
		t.Errorf("MaxConcurrency: got %d, want 5", loaded.MaxConcurrency)
	}
	if loaded.QueueTimeoutSeconds != 90 {
		t.Errorf("QueueTimeoutSeconds: got %d, want 90", loaded.QueueTimeoutSeconds)
	}
	if loaded.RequestCooldownMs != 250 {
		t.Errorf("RequestCooldownMs: got %d, want 250", loaded.RequestCooldownMs)
	}
}

func TestLoadRuntimeConfig_MissingFile(t *testing.T) {
	t.Setenv("RUNTIME_CONFIG_PATH", filepath.Join(t.TempDir(), "nope.json"))

	loaded := LoadRuntimeConfig()
	if loaded.MaxConcurrency != 0 || loaded.QueueTimeoutSeconds != 0 || loaded.RequestCooldownMs != 0 {
		t.Errorf("expected zero-valued config when file missing, got %+v", loaded)
	}
}

func TestLoadRuntimeConfig_CorruptFile(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "broken.json")
	if err := os.WriteFile(path, []byte("{not json"), 0600); err != nil {
		t.Fatalf("failed to write corrupt file: %v", err)
	}
	t.Setenv("RUNTIME_CONFIG_PATH", path)

	loaded := LoadRuntimeConfig()
	if loaded.MaxConcurrency != 0 {
		t.Errorf("expected zero-valued config when file corrupt, got %+v", loaded)
	}
}

func TestRuntimeConfigPath_DefaultVsOverride(t *testing.T) {
	// 默认路径
	os.Unsetenv("RUNTIME_CONFIG_PATH")
	if got := runtimeConfigPath(); got != "./runtime_config.json" {
		t.Errorf("default path: got %q, want ./runtime_config.json", got)
	}

	// 环境变量覆盖
	custom := "/tmp/custom_runtime.json"
	t.Setenv("RUNTIME_CONFIG_PATH", custom)
	if got := runtimeConfigPath(); got != custom {
		t.Errorf("custom path: got %q, want %q", got, custom)
	}
}
```

- [ ] **Step 2: 运行测试，确认失败**

```bash
cd C:\Users\chengsongren\Documents\AgentLibs\2api\yuanbao2api
go test ./api/ -run 'TestSaveAndLoadRuntimeConfig|TestLoadRuntimeConfig_MissingFile|TestLoadRuntimeConfig_CorruptFile|TestRuntimeConfigPath_DefaultVsOverride' -v
```

Expected: 编译失败（`RuntimeConfig` / `SaveRuntimeConfig` / `LoadRuntimeConfig` / `runtimeConfigPath` 未定义）。

- [ ] **Step 3: 实现最小代码以通过测试**

创建 `api/config_persist.go`：

```go
package api

import (
	"encoding/json"
	"os"
)

// RuntimeConfig holds runtime-persisted shared server configuration. Values
// here override env-derived defaults at startup and survive service restarts.
type RuntimeConfig struct {
	MaxConcurrency      int `json:"maxConcurrency,omitempty"`
	QueueTimeoutSeconds int `json:"queueTimeoutSeconds,omitempty"`
	RequestCooldownMs   int `json:"requestCooldownMs,omitempty"`
}

// runtimeConfigPath returns the file path used to persist RuntimeConfig. The
// path can be overridden by the RUNTIME_CONFIG_PATH env var (mainly for tests).
func runtimeConfigPath() string {
	if p := os.Getenv("RUNTIME_CONFIG_PATH"); p != "" {
		return p
	}
	return "./runtime_config.json"
}

// LoadRuntimeConfig reads the persisted config from disk. A missing or
// unparseable file is treated as "no override" and returns the zero value;
// callers must check for non-zero fields before overriding env defaults.
func LoadRuntimeConfig() RuntimeConfig {
	var cfg RuntimeConfig
	data, err := os.ReadFile(runtimeConfigPath())
	if err != nil {
		return cfg // missing file or unreadable -> zero value
	}
	_ = json.Unmarshal(data, &cfg) // corrupt JSON -> zero value
	return cfg
}

// SaveRuntimeConfig writes the config to disk with 0600 permissions. Returns
// an error if the write fails; callers should log and continue (do not abort
// the request just because persistence failed).
func SaveRuntimeConfig(cfg RuntimeConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(runtimeConfigPath(), data, 0600)
}
```

- [ ] **Step 4: 运行测试，确认通过**

```bash
go test ./api/ -run 'TestSaveAndLoadRuntimeConfig|TestLoadRuntimeConfig_MissingFile|TestLoadRuntimeConfig_CorruptFile|TestRuntimeConfigPath_DefaultVsOverride' -v
```

Expected: 全部 PASS。

- [ ] **Step 5: 提交**

```bash
git add api/config_persist.go api/config_persist_test.go
git commit -m "feat(admin-shared): add runtime_config.json persistence layer"
```

---

## Task 2: InitRateLimiter 集成持久化覆盖

**Files:**
- Modify: `api/ratelimit.go:38-67`

**Interfaces:**
- Consumes: `LoadRuntimeConfig()`（来自 Task 1）
- Produces: 现有 `InitRateLimiter()` 签名不变；内部行为新增「runtime_config.json > env > 默认」覆盖

- [ ] **Step 1: 在现有测试目录中追加测试**

在 `api/ratelimit_test.go`（新文件）：

```go
package api

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInitRateLimiter_EnvOnly(t *testing.T) {
	// 确保无持久化文件
	t.Setenv("RUNTIME_CONFIG_PATH", filepath.Join(t.TempDir(), "absent.json"))
	os.Setenv("MAX_CONCURRENCY", "4")
	os.Setenv("QUEUE_TIMEOUT_SECONDS", "60")
	os.Setenv("REQUEST_COOLDOWN_MS", "250")
	defer os.Unsetenv("MAX_CONCURRENCY")
	defer os.Unsetenv("QUEUE_TIMEOUT_SECONDS")
	defer os.Unsetenv("REQUEST_COOLDOWN_MS")

	rl := InitRateLimiter()
	if rl.MaxConcurrency() != 4 {
		t.Errorf("MaxConcurrency: got %d, want 4", rl.MaxConcurrency())
	}
	if rl.QueueTimeout().Seconds() != 60 {
		t.Errorf("QueueTimeout: got %v, want 60s", rl.QueueTimeout())
	}
	if rl.Cooldown().Milliseconds() != 250 {
		t.Errorf("Cooldown: got %v, want 250ms", rl.Cooldown())
	}
}

func TestInitRateLimiter_RuntimeConfigOverridesEnv(t *testing.T) {
	// env 给 2，runtime 给 8 — runtime 应胜出
	os.Setenv("MAX_CONCURRENCY", "2")
	defer os.Unsetenv("MAX_CONCURRENCY")

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "runtime_config.json")
	if err := SaveRuntimeConfig(RuntimeConfig{MaxConcurrency: 8}); err != nil {
		t.Fatalf("SaveRuntimeConfig failed: %v", err)
	}
	t.Setenv("RUNTIME_CONFIG_PATH", path)

	rl := InitRateLimiter()
	if rl.MaxConcurrency() != 8 {
		t.Errorf("MaxConcurrency: got %d, want 8 (runtime config should override env)", rl.MaxConcurrency())
	}
}

func TestInitRateLimiter_ZeroRuntimeValuesIgnored(t *testing.T) {
	// 零值不覆盖 env
	os.Setenv("QUEUE_TIMEOUT_SECONDS", "45")
	defer os.Unsetenv("QUEUE_TIMEOUT_SECONDS")

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "runtime_config.json")
	if err := SaveRuntimeConfig(RuntimeConfig{QueueTimeoutSeconds: 0}); err != nil {
		t.Fatalf("SaveRuntimeConfig failed: %v", err)
	}
	t.Setenv("RUNTIME_CONFIG_PATH", path)

	rl := InitRateLimiter()
	if rl.QueueTimeout().Seconds() != 45 {
		t.Errorf("QueueTimeout: got %v, want 45s (zero runtime value should not override)", rl.QueueTimeout())
	}
}

func TestInitRateLimiter_CooldownZeroIsValid(t *testing.T) {
	// cooldown 的 0 是合法值，但目前 InitRateLimiter 不会从 runtime 写入 0，
	// 因为 LoadRuntimeConfig 返回零值时 InitRateLimiter 不区分 "未设置" 和 "0"。
	// 本测试只是确认：env 给 0 时行为不变（已隐含在 TestInitRateLimiter_EnvOnly）。
	os.Unsetenv("REQUEST_COOLDOWN_MS")
	t.Setenv("RUNTIME_CONFIG_PATH", filepath.Join(t.TempDir(), "absent.json"))
	rl := InitRateLimiter()
	if rl.Cooldown().Milliseconds() != 0 {
		t.Errorf("Cooldown: got %v, want 0ms", rl.Cooldown())
	}
}
```

> ⚠️ **注意**：`requestCooldownMs` 的 0 是合法值，但当前 map-based 检测无法区分 "未发送" 与 "发送了 0"。本次 spec 不解决这个边界——用户通过 panel 保存时永远显式发送当前值，覆盖即可。如果未来要从 panel 清零 cooldown，需要改用 `map[string]json.RawMessage` 检测字段存在性。

- [ ] **Step 2: 运行测试，确认失败**

```bash
go test ./api/ -run 'TestInitRateLimiter' -v
```

Expected: `TestInitRateLimiter_RuntimeConfigOverridesEnv` 失败 — 当前 `InitRateLimiter` 不读 `runtime_config.json`。

- [ ] **Step 3: 修改 `InitRateLimiter`**

修改 `api/ratelimit.go` 的 `InitRateLimiter` 函数（保留原有逻辑），在读取 env 之后构造 RateLimiter 之前叠加持久化值：

```go
func InitRateLimiter() *RateLimiter {
	maxC := getEnvInt("MAX_CONCURRENCY", 1)
	if maxC < 1 {
		maxC = 1
	}
	qTimeout := time.Duration(getEnvInt("QUEUE_TIMEOUT_SECONDS", 120)) * time.Second
	if qTimeout < 0 {
		qTimeout = 120 * time.Second
	}
	cooldown := time.Duration(getEnvInt("REQUEST_COOLDOWN_MS", 0)) * time.Millisecond
	if cooldown < 0 {
		cooldown = 0
	}

	// 持久化覆盖 env 默认值（runtime_config.json > env > 内置默认值）
	if rc := LoadRuntimeConfig(); rc.MaxConcurrency > 0 {
		maxC = rc.MaxConcurrency
		if maxC < 1 {
			maxC = 1
		}
	}
	if rc := LoadRuntimeConfig(); rc.QueueTimeoutSeconds > 0 {
		qTimeout = time.Duration(rc.QueueTimeoutSeconds) * time.Second
	}
	if rc := LoadRuntimeConfig(); rc.RequestCooldownMs > 0 {
		cooldown = time.Duration(rc.RequestCooldownMs) * time.Millisecond
	}

	globalRateLimiter = &RateLimiter{
		sem:            make(chan struct{}, maxC),
		maxConcurrency: maxC,
		queueTimeout:   qTimeout,
		cooldown:       cooldown,
	}

	// Surface the resolved values on the server config for visibility.
	serverConfigLock.Lock()
	serverConfig.MaxConcurrency = maxC
	serverConfig.QueueTimeoutSeconds = int(qTimeout.Seconds())
	serverConfig.RequestCooldownMs = int(cooldown.Milliseconds())
	serverConfigLock.Unlock()

	return globalRateLimiter
}
```

- [ ] **Step 4: 运行测试，确认通过**

```bash
go test ./api/ -run 'TestInitRateLimiter' -v
```

Expected: 全部 PASS。

- [ ] **Step 5: 提交**

```bash
git add api/ratelimit.go api/ratelimit_test.go
git commit -m "feat(admin-shared): init rate limiter reads runtime_config.json overrides"
```

---

## Task 3: HandleSetConfig 改写（map-based + 并发字段持久化）

**Files:**
- Modify: `api/config.go:73-98`（替换 `HandleSetConfig`）
- Modify: `api/config.go`（新增 `toInt` 辅助函数）
- Modify: `api/config_test.go`（追加测试）

**Interfaces:**
- Consumes: `SaveRuntimeConfig()`（来自 Task 1）
- Produces: 现有 `HandleSetConfig(c *gin.Context)` 签名不变；改用 `map[string]interface{}` 按 key 检测

- [ ] **Step 1: 在 config_test.go 追加测试**

打开 `api/config_test.go`，在末尾追加：

```go
func TestHandleSetConfig_PartialUpdateDoesNotZeroOtherFields(t *testing.T) {
	resetServerConfig()

	// 预设：DeepThinking=true
	serverConfigLock.Lock()
	serverConfig.DeepThinking = true
	serverConfigLock.Unlock()

	// 只发 maxConcurrency，不发 deepThinking
	body := `{"maxConcurrency":7}`
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/config", nil)
	c.Request.Body = &readCloser{data: body}
	c.Request.Header.Set("Content-Type", "application/json")

	// 指向临时路径，避免污染真实磁盘
	t.Setenv("RUNTIME_CONFIG_PATH", filepath.Join(t.TempDir(), "rc.json"))

	HandleSetConfig(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d. body=%s", w.Code, w.Body.String())
	}

	cfg := GetServerConfig()
	if cfg.DeepThinking != true {
		t.Errorf("DeepThinking should remain true, got %v", cfg.DeepThinking)
	}
	if cfg.MaxConcurrency != 7 {
		t.Errorf("MaxConcurrency: got %d, want 7", cfg.MaxConcurrency)
	}
}

func TestHandleSetConfig_PersistsRuntimeConfig(t *testing.T) {
	resetServerConfig()
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "rc.json")
	t.Setenv("RUNTIME_CONFIG_PATH", path)

	body := `{"maxConcurrency":3,"queueTimeoutSeconds":80,"requestCooldownMs":400}`
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/config", nil)
	c.Request.Body = &readCloser{data: body}
	c.Request.Header.Set("Content-Type", "application/json")

	HandleSetConfig(c)

	// 文件应被写入
	loaded := LoadRuntimeConfig()
	if loaded.MaxConcurrency != 3 || loaded.QueueTimeoutSeconds != 80 || loaded.RequestCooldownMs != 400 {
		t.Errorf("runtime_config.json not persisted correctly: %+v", loaded)
	}
}
```

需要 import `path/filepath`，确保 `config_test.go` 顶部有 `"path/filepath"`。

- [ ] **Step 2: 运行测试，确认失败**

```bash
go test ./api/ -run 'TestHandleSetConfig_PartialUpdateDoesNotZeroOtherFields|TestHandleSetConfig_PersistsRuntimeConfig' -v
```

Expected: 两条测试失败 — `HandleSetConfig` 当前使用 struct binding 会把 `DeepThinking` 重置为 false。

- [ ] **Step 3: 重写 HandleSetConfig + 添加 toInt**

修改 `api/config.go`，完整替换 `HandleSetConfig` 并在文件末尾（或顶部）新增 `toInt`：

```go
// toInt safely extracts an integer from a JSON-decoded value (numbers come
// back as float64 by default in Go's json package).
func toInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	default:
		return 0, false
	}
}

// HandleSetConfig updates the server configuration. The request body is read
// as a raw map so that omitted fields do NOT zero-out existing values
// (avoids the Go zero-value pitfall when binding partial updates into a
// struct).
func HandleSetConfig(c *gin.Context) {
	var body map[string]interface{}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	serverConfigLock.Lock()
	defer serverConfigLock.Unlock()

	// 原有字段：仅在 key 存在时更新
	if v, ok := body["deepThinking"]; ok {
		if b, ok := v.(bool); ok {
			serverConfig.DeepThinking = b
		}
	}
	if v, ok := body["internetSearch"]; ok {
		if b, ok := v.(bool); ok {
			serverConfig.InternetSearch = b
		}
	}
	if v, ok := body["defaultModel"]; ok {
		if s, ok := v.(string); ok && s != "" {
			serverConfig.DefaultModel = s
		}
	}
	if v, ok := body["agentId"]; ok {
		if s, ok := v.(string); ok && s != "" {
			serverConfig.AgentID = s
		}
	}

	// 新增并发字段
	changed := false
	if v, ok := body["maxConcurrency"]; ok {
		if n, ok := toInt(v); ok && n > 0 {
			serverConfig.MaxConcurrency = n
			changed = true
		}
	}
	if v, ok := body["queueTimeoutSeconds"]; ok {
		if n, ok := toInt(v); ok && n > 0 {
			serverConfig.QueueTimeoutSeconds = n
			changed = true
		}
	}
	if v, ok := body["requestCooldownMs"]; ok {
		if n, ok := toInt(v); ok && n >= 0 {
			serverConfig.RequestCooldownMs = n
			changed = true
		}
	}

	// 持久化
	if changed {
		cfg := RuntimeConfig{
			MaxConcurrency:      serverConfig.MaxConcurrency,
			QueueTimeoutSeconds: serverConfig.QueueTimeoutSeconds,
			RequestCooldownMs:   serverConfig.RequestCooldownMs,
		}
		if err := SaveRuntimeConfig(cfg); err != nil {
			log.Printf("保存运行时配置失败: %v", err)
		}
	}

	c.JSON(http.StatusOK, serverConfig)
}
```

需要 import `"log"`。

- [ ] **Step 4: 运行全部 config + ratelimit 测试**

```bash
go test ./api/ -run 'TestHandleSetConfig|TestInitRateLimiter|TestSaveAndLoadRuntimeConfig|TestLoadRuntimeConfig|TestRuntimeConfigPath|TestServerConfigData|TestSyncAgentID|TestGetAgentID' -v
```

Expected: 全部 PASS。注意 `TestHandleSetConfig_AcceptsAgentID`（旧测试）应继续通过，因为发送 `{"agentId":"..."}` 现在被 map 检测正确处理。

- [ ] **Step 5: 提交**

```bash
git add api/config.go api/config_test.go
git commit -m "feat(admin-shared): HandleSetConfig supports partial updates + persists concurrency"
```

---

## Task 4: HandleRestart 端点

**Files:**
- Create: `api/restart.go`
- Test: `api/restart_test.go`
- Modify: `main.go:78`（注册路由）

**Interfaces:**
- Produces: `HandleRestart(c *gin.Context)` 处理 `POST /api/restart`
- 测试通过 `exitFn` 变量替换实际退出行为

- [ ] **Step 1: 写失败的测试**

创建 `api/restart_test.go`：

```go
package api

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestHandleRestart_RespondsAndCallsExit(t *testing.T) {
	// 替换 exitFn，避免真退出
	var called int32
	origExit := exitFn
	exitFn = func(code int) {
		atomic.StoreInt32(&called, int32(code))
	}
	defer func() { exitFn = origExit }()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/restart", nil)
	HandleRestart(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	// 等异步退出（HandleRestart 启动 goroutine，500ms 后调用 exitFn）
	// 简化：直接读 atomic 变量，循环等待
	deadline := time.After(2 * time.Second)
	for {
		if atomic.LoadInt32(&called) != 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("exitFn was not called within 2s")
		default:
			time.Sleep(20 * time.Millisecond)
		}
	}
	if got := atomic.LoadInt32(&called); got != 0 {
		t.Errorf("expected exit code 0, got %d", got)
	}
}
```

需要 import `"time"` 和 `"sync/atomic"`。

- [ ] **Step 2: 运行测试，确认失败**

```bash
go test ./api/ -run 'TestHandleRestart' -v
```

Expected: 编译失败 — `exitFn` 未定义。

- [ ] **Step 3: 实现 HandleRestart**

创建 `api/restart.go`：

```go
package api

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
)

// exitFn is the function HandleRestart uses to terminate the process. It is a
// package-level variable so tests can intercept it without killing the test
// runner. Defaults to os.Exit.
var exitFn = os.Exit

// HandleRestart responds with 200, then asynchronously terminates the process
// after a small delay so the response can flush. The process is expected to
// be wrapped by restart.bat (Windows) or a process manager, which will
// relaunch it. New runtime values take effect on the next start.
func HandleRestart(c *gin.Context) {
	log.Println("收到重启请求，服务即将退出...")
	c.JSON(http.StatusOK, gin.H{"status": "restarting"})
	go func() {
		time.Sleep(500 * time.Millisecond)
		exitFn(0)
	}()
}
```

修改 `main.go:78` 后新增一行路由注册：

```go
config.GET("/env", api.HandleEnv)
config.GET("/logs", api.HandleLogs)
config.POST("/restart", api.HandleRestart)  // 新增
```

- [ ] **Step 4: 运行测试，确认通过**

```bash
go test ./api/ -run 'TestHandleRestart' -v
```

Expected: PASS。

- [ ] **Step 5: 全量编译 + 全量测试**

```bash
go build -o main.exe .
go test ./... -v
```

Expected: 编译成功，全部测试 PASS。

- [ ] **Step 6: 提交**

```bash
git add api/restart.go api/restart_test.go main.go
git commit -m "feat(admin-shared): add POST /api/restart endpoint"
```

---

## Task 5: 前端 — 配置面板并发参数 UI

**Files:**
- Modify: `public/index.html:115-150`（在"运行时配置"section 之后插入新 section）
- Modify: `public/app.js`（追加 `loadConcurrency` / `saveConcurrency` / `restartService` 函数；扩展 `loadConfig` / `applyConfigToUI`）
- Modify: `public/style.css`（追加 `.btn-danger` 样式）

**Interfaces:**
- Consumes: `GET /api/config`（已有）；`POST /api/config`（来自 Task 3）；`POST /api/restart`（来自 Task 4）
- Produces: 三个数字输入框 + 保存按钮 + 重启按钮

- [ ] **Step 1: 在 index.html 插入新 section**

打开 `public/index.html`，定位到 `<div class="section-title">运行时配置</div>` 这一行所在的 `<div class="section">` 结束位置。在它之后（"功能配置"之前）插入：

```html
<div class="section">
    <div class="section-title">并发参数（需重启生效）</div>
    <div class="config-row">
        <span class="config-label">MAX_CONCURRENCY:</span>
        <input type="number" id="maxConcurrencyInput" min="1" class="config-input" style="width:100px;">
    </div>
    <div class="config-row">
        <span class="config-label">QUEUE_TIMEOUT_SECONDS:</span>
        <input type="number" id="queueTimeoutInput" min="1" class="config-input" style="width:100px;">
    </div>
    <div class="config-row">
        <span class="config-label">REQUEST_COOLDOWN_MS:</span>
        <input type="number" id="cooldownInput" min="0" class="config-input" style="width:100px;">
    </div>
    <div style="color:#ffa500;font-size:12px;margin:8px 0;">⚠ 修改后需重启服务才能生效</div>
    <button class="btn" onclick="App.saveConcurrency()">保存并发参数</button>
    <button class="btn btn-danger" onclick="App.restartService()" style="margin-left:8px;">🔄 重启服务</button>
    <span id="restartStatus" style="margin-left:12px;font-size:13px;"></span>
</div>
```

- [ ] **Step 2: 在 style.css 追加 .btn-danger**

打开 `public/style.css`，找到 `.btn` 样式之后追加：

```css
.btn-danger {
    background: #d33;
    color: #fff;
}
.btn-danger:hover {
    background: #b22;
}
```

- [ ] **Step 3: 在 app.js 扩展 loadConfig + applyConfigToUI**

打开 `public/app.js`，在 `applyConfigToUI` 函数末尾追加（保留原逻辑不变）：

```js
const mc = document.getElementById('maxConcurrencyInput');
if (mc) mc.value = this.config.maxConcurrency ?? '';
const qt = document.getElementById('queueTimeoutInput');
if (qt) qt.value = this.config.queueTimeoutSeconds ?? '';
const cd = document.getElementById('cooldownInput');
if (cd) cd.value = this.config.requestCooldownMs ?? '';
```

- [ ] **Step 4: 在 app.js 末尾追加新函数**

打开 `public/app.js`，在 `}` 之前（`App` 对象的最后一个方法 `loadLogs` 之后）插入：

```js
    async saveConcurrency() {
        const maxC = parseInt(document.getElementById('maxConcurrencyInput').value);
        const qTimeout = parseInt(document.getElementById('queueTimeoutInput').value);
        const cooldown = parseInt(document.getElementById('cooldownInput').value);

        if (!maxC || maxC < 1) {
            alert('MAX_CONCURRENCY 必须 ≥ 1');
            return;
        }
        if (!qTimeout || qTimeout < 1) {
            alert('QUEUE_TIMEOUT_SECONDS 必须 ≥ 1');
            return;
        }
        if (isNaN(cooldown) || cooldown < 0) {
            alert('REQUEST_COOLDOWN_MS 必须 ≥ 0');
            return;
        }

        try {
            const res = await fetch('/api/config', {
                method: 'POST',
                headers: this._authHeaders(),
                body: JSON.stringify({
                    maxConcurrency: maxC,
                    queueTimeoutSeconds: qTimeout,
                    requestCooldownMs: cooldown,
                }),
            });
            if (!res.ok) {
                alert('保存失败: HTTP ' + res.status);
                return;
            }
            alert('已保存。点击"重启服务"按钮生效。');
        } catch (e) {
            alert('保存失败: ' + e.message);
        }
    },

    async restartService() {
        if (!confirm('确认重启服务？所有进行中的请求会被中断。')) return;

        const statusEl = document.getElementById('restartStatus');
        statusEl.textContent = '重启中...';
        statusEl.style.color = '#ffa500';

        try {
            const res = await fetch('/api/restart', {
                method: 'POST',
                headers: this._authHeaders(),
            });
            if (!res.ok) {
                statusEl.textContent = '重启请求失败: HTTP ' + res.status;
                statusEl.style.color = '#f44';
                return;
            }
        } catch (e) {
            // 网络中断属于正常情况——服务可能已退出
            console.log('重启请求网络中断（预期行为）:', e.message);
        }

        // 轮询 /health 检测恢复
        const deadline = Date.now() + 30000;
        const poll = async () => {
            if (Date.now() > deadline) {
                statusEl.textContent = '重启超时（30s），请手动检查';
                statusEl.style.color = '#f44';
                return;
            }
            try {
                const r = await fetch('/health', { cache: 'no-store' });
                if (r.ok) {
                    statusEl.textContent = '✅ 服务已恢复';
                    statusEl.style.color = '#0f0';
                    // 重新加载配置 + 状态
                    this.loadConfig();
                    this.loadStatus();
                    return;
                }
            } catch (e) {
                // 服务还启动中
            }
            setTimeout(poll, 1000);
        };
        setTimeout(poll, 1500); // 给重启 bat 留启动时间
    },
```

- [ ] **Step 5: 手动 smoke 测试**

启动服务：

```bash
go build -o main.exe .
./main.exe
```

或通过 `restart.bat` 启动。浏览器打开 `http://localhost:3000`，进入"⚙️ 配置"标签页：

1. 应能看到"并发参数"section，三个输入框已自动填充当前值
2. 修改 `MAX_CONCURRENCY` 为 5，点击"保存并发参数"
3. 弹窗显示"已保存..."
4. 验证 `runtime_config.json` 已写入：

```bash
cat runtime_config.json
```

应见 `{"maxConcurrency":5,...}`

5. 点击"🔄 重启服务"，确认后页面应显示"重启中..."
6. 1-3 秒内状态变 "✅ 服务已恢复"
7. 验证 `/api/status` 返回的 `maxConcurrency` 是新值
8. 验证 `/api/env` 返回的 `maxConcurrency` 也是新值

- [ ] **Step 6: 提交**

```bash
git add public/index.html public/app.js public/style.css
git commit -m "feat(admin-shared): add concurrency config UI + restart button"
```

---

## Task 6: restart.bat 启动器

**Files:**
- Create: `restart.bat`

**Interfaces:**
- 启动 `main.exe`，退出后等 5 秒重启

- [ ] **Step 1: 创建 restart.bat**

创建项目根目录的 `restart.bat`：

```bat
@echo off
title yuanbao2api
echo ========================================
echo  元宝2API 服务启动器（自动重启循环）
echo  关闭此窗口即可停止服务
echo ========================================
:loop
echo [%date% %time%] 启动服务...
.\main.exe
echo [%date% %time%] 服务已退出，5 秒后重启...
timeout /t 5 >nul
goto loop
```

- [ ] **Step 2: 验证脚本**

```bash
# 在 Windows 上
cmd /c "cd /d C:\path\to\yuanbao2api && restart.bat"
```

或手动双击 `restart.bat`。验证：
- 启动正常
- 在管理面板点"重启服务"，服务确实重启（窗口不关闭，新一轮 main.exe 启动）

- [ ] **Step 3: 提交**

```bash
git add restart.bat
git commit -m "feat(admin-shared): add Windows auto-restart wrapper script"
```

---

## Self-Review（执行前再过一遍）

**Spec 覆盖检查：**
- 持久化层（Task 1） ✓
- env 覆盖（Task 2） ✓
- HandleSetConfig 改写（Task 3）✓
- 重启端点（Task 4）✓
- 前端 UI（Task 5）✓
- restart.bat（Task 6）✓

**类型一致性检查：**
- `RuntimeConfig` 字段：`MaxConcurrency`、`QueueTimeoutSeconds`、`RequestCooldownMs` ✓
- `SaveRuntimeConfig(RuntimeConfig) error` / `LoadRuntimeConfig() RuntimeConfig` ✓
- `HandleRestart(c *gin.Context)` / `HandleSetConfig(c *gin.Context)` ✓
- `toInt(interface{}) (int, bool)` ✓
- `exitFn func(int)` ✓

**Placeholder 检查：** 已确认无 TBD/TODO/类似表述。

**风险点：**
- Task 5 中 `setTimeout(poll, 1500)` 与 `restart.bat` 的 5 秒等待可能不够——浏览器 fetch 在 `restartService` 内可能被阻塞到服务关闭后才返回。已通过 `try/catch` 吞掉网络错误处理。
- `requestCooldownMs=0` 当前无法在 panel 写入"清零"（map 检测要求 changed=true 但 cooldown=0 不触发）。本次 spec 不解决，留作 future improvement（用 map[string]json.RawMessage 检测字段存在性）。
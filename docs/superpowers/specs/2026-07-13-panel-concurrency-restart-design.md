# 管理面板并发参数配置 + 重启服务

**日期**: 2026-07-13
**状态**: 已批准设计，待实现
**范围**: 多 provider 远景下的「共享管理面板层」子集

## 目标

让用户可以在管理面板（`/`）中修改并发控制参数（`MAX_CONCURRENCY`、`QUEUE_TIMEOUT_SECONDS`、`REQUEST_COOLDOWN_MS`），持久化到磁盘，然后通过管理面板重启服务使配置生效。

不追求热更——修改后需点"重启服务"按钮生效。

## 设计概要

### 数据流

```
管理面板输入 → POST /api/config  ->  HandleSetConfig
  → 提取并发字段 → SaveRuntimeConfig() 写入 runtime_config.json
  → 用户点"重启服务" → POST /api/restart
  → HandleRestart 返回 {"status":"restarting"}
  → time.Sleep(500ms) → os.Exit(0)
  → restart.bat 检测退出后重新拉起 main.exe
  → InitRateLimiter() 启动时读 runtime_config.json → 覆盖环境变量 → 生效
```

### 持久化文件

- 文件名：`runtime_config.json`（与 main.exe 同目录）
- 格式：JSON，只包含三个并发字段
- 生效优先级：**`runtime_config.json` > 环境变量**
- `.gitignore` 照常忽略 `runtime_config.json`（非仓库文件，仅存在于部署环境）

示例内容：
```json
{
  "maxConcurrency": 3,
  "queueTimeoutSeconds": 120,
  "requestCooldownMs": 500
}
```

## 后端改动

### 1. 新文件 `api/config_persist.go`

职责：读写 `runtime_config.json`。

- `RuntimeConfig` struct：三个并发字段，omitempty
- `runtimeConfigPath()` 返回 `"./runtime_config.json"`（可通过环境变量 `RUNTIME_CONFIG_PATH` 覆盖）
- `LoadRuntimeConfig() RuntimeConfig`：读文件 → JSON 反序列化；文件不存在或解析失败返回零值（静默忽略）
- `SaveRuntimeConfig(cfg RuntimeConfig) error`：JSON 序列化 → 写文件（`0600` 权限）

### 2. `api/ratelimit.go` — `InitRateLimiter` 修改

现有流程：读 env → 构造 RateLimiter。

新流程：
1. 读 env 得默认值
2. 调 `LoadRuntimeConfig()` 获取持久化配置
3. 如果持久化配置中有非零值，覆盖对应的默认值
4. 用最终值构造 RateLimiter
5. 更新 `serverConfig` 上的展示字段

```go
func InitRateLimiter() *RateLimiter {
    maxC := getEnvInt("MAX_CONCURRENCY", 1)
    qTimeout := time.Duration(getEnvInt("QUEUE_TIMEOUT_SECONDS", 120)) * time.Second
    cooldown := time.Duration(getEnvInt("REQUEST_COOLDOWN_MS", 0)) * time.Millisecond

    // 持久化覆盖
    if rc := LoadRuntimeConfig(); rc.MaxConcurrency > 0 {
        maxC = rc.MaxConcurrency
    }
    if rc.QueueTimeoutSeconds > 0 {
        qTimeout = time.Duration(rc.QueueTimeoutSeconds) * time.Second
    }
    if rc.RequestCooldownMs > 0 {
        cooldown = time.Duration(rc.RequestCooldownMs) * time.Millisecond
    }

    // ... 余下不变（构造 RateLimiter、更新 serverConfig）
}
```

### 3. `api/config.go` — `HandleSetConfig` 修改

#### 零值问题

现有的 `HandleSetConfig` 通过 `ShouldBindJSON` 反序列化到 `ServerConfigData` struct。如果前端只发部分字段（如 `{"maxConcurrency":3}`），未传递的布尔字段会因 Go 零值为 `false`，错误覆盖已有配置。

**解决方案**：先用 `json.RawMessage` 读取原始 body，再按需提取字段，避免零值覆盖。

#### 修改方法

改用 `map[string]interface{}` + `json.Unmarshal` 按 key 存在性检测，仅更新请求中显式携带的字段：

```go
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
        serverConfig.DeepThinking = v.(bool)
    }
    if v, ok := body["internetSearch"]; ok {
        serverConfig.InternetSearch = v.(bool)
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
    var changed bool
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

    // 持久化到 runtime_config.json
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

新增一个辅助函数 `toInt` 用于从 `interface{}` 安全提取 int（JSON 数字解析为 `float64`）：

```go
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
```

**向后兼容**：此改动不影响现有前端行为，因为 `deepThinking` / `internetSearch` 等 key 仍按原来的名称处理。前端现存的所有 `POST /api/config` 调用继续正常工作。

`ServerConfigData` 中的 `MaxConcurrency` / `QueueTimeoutSeconds` / `RequestCooldownMs` 字段维持不变，`HandleGetConfig` 和 `HandleSetConfig` 均能覆盖它们。

### 4. 新文件 `api/restart.go`

```go
func HandleRestart(c *gin.Context) {
    c.JSON(http.StatusOK, gin.H{"status": "restarting"})
    go func() {
        time.Sleep(500 * time.Millisecond) // 给响应发送窗口
        log.Println("收到重启请求，服务即将退出...")
        os.Exit(0)
    }()
}
```

### 5. `main.go` — 新增路由

在 `/api` 组下注册：

```go
config.POST("/restart", api.HandleRestart)
```

## 前端改动

### 6. `public/index.html` — 配置面板新增区域

在"运行时配置"section 之后 / "功能配置"之前，新增"并发参数"区间：

```
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
  <div style="color:#ffa500;font-size:12px;margin:8px 0;">⚠ 修改后需重启服务生效</div>
  <button class="btn" onclick="App.saveConcurrency()">保存并发参数</button>
  <button class="btn btn-danger" onclick="App.restartService()" style="margin-left:8px;">🔄 重启服务</button>
</div>
```

### 7. `public/app.js` — 新增函数

- `loadConfig()` 中填充输入框：`maxConcurrencyInput.value = config.maxConcurrency` 等
- `saveConcurrency()`：读三个输入框值 → 只发送三个并发字段的 JSON 到 `POST /api/config`（后端按 key 存在性检测，不会零值覆盖其他字段）
- `restartService()`：`confirm()` 确认 → `POST /api/restart` → 提示"服务重启中"，页面轮询 `/health` 直到恢复
- 新增 `btn-danger` 样式（红色按钮）

由于后端改用 `map[string]interface{}` 按 key 存在性更新，前端可以安全地只发部分字段。现有的 `saveConfig()` / `toggleFeature()` / `saveAgentId()` 不受影响。

## 重启脚本

### 8. `restart.bat`

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

用户从此双击 `restart.bat` 启动，而非直接运行 `main.exe`。

## 约束与边界

- `runtime_config.json` 不存在时不报错，静默回退到环境变量
- 负数或零值视为"未设置"，不覆盖 env
- 重启端点不验证 Cookie 有效性等——它只负责退出进程
- 重启时如果存在挂起的请求，**它们会中断**（`os.Exit`），这是设计上接受的（配置变更本来就应避开业务高峰）

## 未来兼容（Multi-Provider 远景）

**项目长期目标**：从单一元宝 wrapper 演化为多 provider web2api 聚合器。统一管理面板可配置每家反代；模型路由按 provider 划分。本 spec 是这条路径上的子集。

### 本 spec 中可直接复用的部件

| 部件 | 未来多 provider 时的去向 |
|------|------------------------|
| `RuntimeConfig` struct + Load/Save 函数 | 升级为全局共享配置 schema（新增 API_KEY、PORT、GIN_MODE 等） |
| `runtime_config.json` 文件路径约定 | 路径不变；新增 `providers/<id>.json` 用于各家专属配置 |
| `InitRateLimiter` env + file override 模式 | 推广到其他初始化逻辑（InitSessionManager、InitRouteTable 等） |
| `POST /api/restart` 端点 + `restart.bat` | 完全复用，零改动 |
| `handleSetConfig` 用 `map[string]interface{}` 按 key 检测 | 沿用：未来 Provider 专属端点（如 `/api/providers/yuanbao/config`）也用同一模式 |

### 本 spec 中需要警惕、不应过度耦合的部件

| 部件 | 提醒 |
|------|------|
| `ServerConfigData` 的 `AgentID` 等元宝专属字段 | 未来需迁出到 `ProviderYuanbaoConfig`，但本次**不动** |
| `HandleSetConfig` 当前聚合所有共享+元宝字段 | 未来需按 provider 拆分（`/api/config` 共享层 + `/api/providers/:id/config` 各自一层）；本次**保持聚合**，但用 map-based 检测避免阻塞拆分 |
| `public/index.html` 把 AgentID 直接放进"运行时配置"section | 未来属于 `Providers/元宝/`，但 UI 重组是另一次 PR |

### 当前 spec 明确**不做**的事

- 不引入 Provider 接口/注册表
- 不修改 `/v1/chat/completions` 路由分发逻辑
- 不改造现有 `yuanbao/` 包结构
- 不预先抽象"反代适配器"框架

这些都留到 future multi-provider 设计的 spec。

### 标记

PR/Commit 标题前缀建议 `feat(admin-shared):` 以表明属于「共享管理面板层」而非「Provider 层」，便于未来按层 cherry-pick。

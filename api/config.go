package api

import (
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"

	"github.com/gin-gonic/gin"
)

// ServerConfig holds the dynamic server configuration (same as internal/config but for API layer)
var (
	serverConfig     = &ServerConfigData{DeepThinking: false, InternetSearch: false, DefaultModel: "deep_seek_v3"}
	serverConfigLock sync.RWMutex
)

// ServerConfigData represents the server configuration
type ServerConfigData struct {
	DeepThinking   bool   `json:"deepThinking"`
	InternetSearch bool   `json:"internetSearch"`
	DefaultModel   string `json:"defaultModel"`

	// Rate limiting (resolved from persisted overrides/env at startup; informational here).
	MaxConcurrency      int `json:"maxConcurrency"`
	QueueTimeoutSeconds int `json:"queueTimeoutSeconds"`
	RequestCooldownMs   int `json:"requestCooldownMs"`

	// AgentID — runtime-settable via /api/config
	AgentID string `json:"agentId"`
}

// getEnvInt reads an integer environment variable, falling back to def on
// missing/invalid values.
func getEnvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// HandleStatus returns live concurrency/queue stats for observability.
func HandleStatus(c *gin.Context) {
	rl := GetRateLimiter()
	if rl == nil {
		c.JSON(http.StatusOK, gin.H{
			"maxConcurrency":      0,
			"queueTimeoutSeconds": 0,
			"requestCooldownMs":   0,
			"inflight":            0,
			"waiting":             0,
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"maxConcurrency":      rl.MaxConcurrency(),
		"queueTimeoutSeconds": int(rl.QueueTimeout().Seconds()),
		"requestCooldownMs":   int(rl.Cooldown().Milliseconds()),
		"inflight":            rl.Inflight(),
		"waiting":             rl.Waiting(),
	})
}

// HandleGetConfig returns the current server configuration
func HandleGetConfig(c *gin.Context) {
	serverConfigLock.RLock()
	defer serverConfigLock.RUnlock()
	c.JSON(http.StatusOK, serverConfig)
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

	var maxConcurrency *int
	if v, ok := body["maxConcurrency"]; ok {
		n, valid := toInt(v)
		if !valid || n < 1 || n > 1000 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "maxConcurrency out of range (1..1000)"})
			return
		}
		maxConcurrency = &n
	}

	var queueTimeoutSeconds *int
	if v, ok := body["queueTimeoutSeconds"]; ok {
		n, valid := toInt(v)
		if !valid || n < 1 || n > 3600 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "queueTimeoutSeconds out of range (1..3600)"})
			return
		}
		queueTimeoutSeconds = &n
	}

	var requestCooldownMs *int
	if v, ok := body["requestCooldownMs"]; ok {
		n, valid := toInt(v)
		if !valid || n < 0 || n > 60000 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "requestCooldownMs out of range (0..60000)"})
			return
		}
		requestCooldownMs = &n
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

	changed := false
	if maxConcurrency != nil {
		serverConfig.MaxConcurrency = *maxConcurrency
		changed = true
	}
	if queueTimeoutSeconds != nil {
		serverConfig.QueueTimeoutSeconds = *queueTimeoutSeconds
		changed = true
	}
	if requestCooldownMs != nil {
		serverConfig.RequestCooldownMs = *requestCooldownMs
		changed = true
	}

	if changed {
		maxConcurrency := serverConfig.MaxConcurrency
		queueTimeoutSeconds := serverConfig.QueueTimeoutSeconds
		requestCooldownMs := serverConfig.RequestCooldownMs
		cfg := RuntimeConfig{
			MaxConcurrency:      &maxConcurrency,
			QueueTimeoutSeconds: &queueTimeoutSeconds,
			RequestCooldownMs:   &requestCooldownMs,
		}
		if err := SaveRuntimeConfig(cfg); err != nil {
			log.Printf("保存运行时配置失败: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to persist runtime config: " + err.Error()})
			return
		}
	}

	c.JSON(http.StatusOK, serverConfig)
}

// toInt safely extracts an integer from a JSON-decoded value (numbers come
// back as float64 by default in Go's json package).
func toInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case float64:
		converted := int(n)
		if float64(converted) != n {
			return 0, false
		}
		return converted, true
	case int:
		return n, true
	default:
		return 0, false
	}
}

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

// GetServerConfig returns a copy of the current server config
func GetServerConfig() ServerConfigData {
	serverConfigLock.RLock()
	defer serverConfigLock.RUnlock()
	return *serverConfig
}

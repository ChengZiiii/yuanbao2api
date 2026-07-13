package api

import (
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

	// Rate limiting (read from env at startup; informational here).
	MaxConcurrency     int `json:"maxConcurrency"`
	QueueTimeoutSeconds int `json:"queueTimeoutSeconds"`
	RequestCooldownMs  int `json:"requestCooldownMs"`

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
			"maxConcurrency":     0,
			"queueTimeoutSeconds": 0,
			"requestCooldownMs":  0,
			"inflight":           0,
			"waiting":            0,
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"maxConcurrency":     rl.MaxConcurrency(),
		"queueTimeoutSeconds": int(rl.QueueTimeout().Seconds()),
		"requestCooldownMs":  int(rl.Cooldown().Milliseconds()),
		"inflight":           rl.Inflight(),
		"waiting":            rl.Waiting(),
	})
}

// HandleGetConfig returns the current server configuration
func HandleGetConfig(c *gin.Context) {
	serverConfigLock.RLock()
	defer serverConfigLock.RUnlock()
	c.JSON(http.StatusOK, serverConfig)
}

// HandleSetConfig updates the server configuration
func HandleSetConfig(c *gin.Context) {
	var req ServerConfigData
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	serverConfigLock.Lock()
	defer serverConfigLock.Unlock()

	if req.DeepThinking != serverConfig.DeepThinking {
		serverConfig.DeepThinking = req.DeepThinking
	}
	if req.InternetSearch != serverConfig.InternetSearch {
		serverConfig.InternetSearch = req.InternetSearch
	}
	if req.DefaultModel != "" {
		serverConfig.DefaultModel = req.DefaultModel
	}
	if req.AgentID != "" {
		serverConfig.AgentID = req.AgentID
	}

	c.JSON(http.StatusOK, serverConfig)
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

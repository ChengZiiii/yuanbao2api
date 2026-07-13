package api

import (
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
)

// maskCookie returns the first 8 characters followed by "****".
func maskCookie(cookie string) string {
	if len(cookie) <= 8 {
		return cookie
	}
	return cookie[:8] + "****"
}

func maskKey(key string) string {
	if len(key) <= 4 {
		return "****"
	}
	return key[:4] + "****"
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
	apiKey := os.Getenv("API_KEY")

	c.JSON(http.StatusOK, gin.H{
		"port":                os.Getenv("PORT"),
		"ginMode":             os.Getenv("GIN_MODE"),
		"maxConcurrency":      maxC,
		"queueTimeoutSeconds": qTimeout,
		"requestCooldownMs":   cooldown,
		"yuanbaoAgentId":      getAgentID(),
		"yuanbaoCookie":       maskCookie(cookie),
		"apiKey":              apiKey,
	})
}

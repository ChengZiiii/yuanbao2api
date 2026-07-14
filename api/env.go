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

	cookie := EffectiveYuanbaoCookie()
	source := EffectiveYuanbaoCookieSource()
	apiKey := os.Getenv("API_KEY")

	// Per-component masks mirror the assembled cookie so the panel can show
	// each half independently. Each value is "" when the corresponding half
	// is empty.
	var hyToken, hyUser string
	serverConfigLock.RLock()
	yc := serverConfig.YuanbaoCookie
	serverConfigLock.RUnlock()
	if yc != nil {
		hyToken = yc.HyToken
		hyUser = yc.HyUser
	}
	// When runtime override is not providing a value, fall back to the env
	// string so the masks at least reflect what would actually be sent.
	if hyToken == "" && hyUser == "" {
		if env := os.Getenv("YUANBAO_COOKIE"); env != "" {
			parsed := &YuanbaoCookie{}
			if err := parsed.parseLegacyString(env); err == nil {
				hyToken = parsed.HyToken
				hyUser = parsed.HyUser
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"port":                os.Getenv("PORT"),
		"ginMode":             os.Getenv("GIN_MODE"),
		"maxConcurrency":      maxC,
		"queueTimeoutSeconds": qTimeout,
		"requestCooldownMs":   cooldown,
		"yuanbaoAgentId":      getAgentID(),
		"yuanbaoCookie":       maskCookie(cookie),
		"yuanbaoHyToken":      maskCookie(hyToken),
		"yuanbaoHyUser":       maskCookie(hyUser),
		"cookieSource":        string(source),
		"apiKey":              apiKey,
	})
}

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

// HandleEnv returns non-sensitive environment variables, masked cookie
// fields, and a multi-provider summary so the management panel can
// render both the legacy single-cookie table and the new "站点管理"
// tab without an extra fetch.
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

	// Per-component masks mirror the assembled cookie so the panel can
	// show each half independently. Each value is "" when the
	// corresponding half is empty.
	var hyToken, hyUser string
	serverConfigLock.RLock()
	yc := serverConfig.YuanbaoCookie
	serverConfigLock.RUnlock()
	if yc != nil {
		hyToken = yc.HyToken
		hyUser = yc.HyUser
	}
	// When runtime override is not providing a value, fall back to the
	// env string so the masks at least reflect what would actually be
	// sent.
	if hyToken == "" && hyUser == "" {
		if env := os.Getenv("YUANBAO_COOKIE"); env != "" {
			parsed := &YuanbaoCookie{}
			if err := parsed.parseLegacyString(env); err == nil {
				hyToken = parsed.HyToken
				hyUser = parsed.HyUser
			}
		}
	}

	// Build the multi-provider summary by walking serverConfig.Providers.
	// Each entry exposes name / enabled / cookieSource / masked cookie /
	// masked hyToken / masked hyUser / agentId mask / concurrency. The
	// default-provider top-level fields stay yuanbao-shaped so the
	// existing dashboard cards continue to render unchanged.
	providers := map[string]map[string]interface{}{}
	serverConfigLock.RLock()
	defaultProvider := serverConfig.DefaultProvider
	if defaultProvider == "" {
		defaultProvider = "yuanbao"
	}
	providersCopy := map[string]ProviderConfig{}
	for k, v := range serverConfig.Providers {
		providersCopy[k] = v
	}
	serverConfigLock.RUnlock()

	for name, p := range providersCopy {
		entry := map[string]interface{}{
			"name":       name,
			"enabled":    p.Enabled != nil && *p.Enabled,
			"configured": p.Cookie != nil && (p.Cookie.HyToken != "" || p.Cookie.HyUser != ""),
		}
		// Cookie source resolution: runtime override wins over env;
		// mirror EffectiveYuanbaoCookieSource for the yuanbao
		// provider so the panel sees a coherent picture.
		if name == "yuanbao" {
			entry["cookieSource"] = string(source)
			entry["yuanbaoCookie"] = maskCookie(cookie)
			entry["yuanbaoHyToken"] = maskCookie(hyToken)
			entry["yuanbaoHyUser"] = maskCookie(hyUser)
		} else {
			entry["cookieSource"] = "none"
		}
		if p.AgentID != nil {
			entry["agentId"] = maskKey(*p.AgentID)
		}
		if p.MaxConcurrency != nil {
			entry["maxConcurrency"] = *p.MaxConcurrency
		} else {
			entry["maxConcurrency"] = maxC
		}
		if p.QueueTimeoutSeconds != nil {
			entry["queueTimeoutSeconds"] = *p.QueueTimeoutSeconds
		} else {
			entry["queueTimeoutSeconds"] = qTimeout
		}
		if p.RequestCooldownMs != nil {
			entry["requestCooldownMs"] = *p.RequestCooldownMs
		} else {
			entry["requestCooldownMs"] = cooldown
		}
		providers[name] = entry
	}

	// Always include yuanbao in the summary even when no entry has
	// been persisted yet — the panel uses it as the default site.
	if _, ok := providers["yuanbao"]; !ok {
		providers["yuanbao"] = map[string]interface{}{
			"name":                "yuanbao",
			"enabled":             true,
			"configured":          false,
			"cookieSource":        string(source),
			"yuanbaoCookie":       maskCookie(cookie),
			"yuanbaoHyToken":      maskCookie(hyToken),
			"yuanbaoHyUser":       maskCookie(hyUser),
			"maxConcurrency":      maxC,
			"queueTimeoutSeconds": qTimeout,
			"requestCooldownMs":   cooldown,
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
		"defaultProvider":     defaultProvider,
		"providers":           providers,
	})
}
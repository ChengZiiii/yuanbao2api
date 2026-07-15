package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestHandleEnv_MultiProviderSummary exercises the new providers[] /
// defaultProvider fields in the /api/env response, alongside the
// legacy yuanbaoCookie / yuanbaoHyToken / yuanbaoHyUser / cookieSource
// fields which the dashboard still renders.
func TestHandleEnv_MultiProviderSummary(t *testing.T) {
	resetServerConfig()
	os.Unsetenv("YUANBAO_COOKIE")

	// Seed Providers map with one enabled entry.
	enabled := true
	maxC := 3
	serverConfigLock.Lock()
	serverConfig.Providers = map[string]ProviderConfig{
		"yuanbao": {
			Enabled:        &enabled,
			MaxConcurrency: &maxC,
		},
	}
	serverConfig.DefaultProvider = "yuanbao"
	serverConfigLock.Unlock()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/env", nil)
	HandleEnv(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, body=%s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["defaultProvider"] != "yuanbao" {
		t.Errorf("defaultProvider: %v", resp["defaultProvider"])
	}
	providers, ok := resp["providers"].(map[string]interface{})
	if !ok {
		t.Fatalf("providers missing or wrong type: %T", resp["providers"])
	}
	yuanbao, ok := providers["yuanbao"].(map[string]interface{})
	if !ok {
		t.Fatalf("providers.yuanbao missing")
	}
	if yuanbao["enabled"] != true {
		t.Errorf("providers.yuanbao.enabled: %v", yuanbao["enabled"])
	}
	if int(yuanbao["maxConcurrency"].(float64)) != 3 {
		t.Errorf("providers.yuanbao.maxConcurrency: %v", yuanbao["maxConcurrency"])
	}
	// Legacy fields must remain populated for the dashboard cards.
	if _, has := resp["yuanbaoCookie"]; !has {
		t.Errorf("legacy yuanbaoCookie missing")
	}
	if _, has := resp["cookieSource"]; !has {
		t.Errorf("legacy cookieSource missing")
	}
}

// TestHandleEnv_EnvFallbackReportsEnvSource covers the spec scenario
// "env 兜底报告 env 来源": when the runtime cookie is unset but env
// has one, /api/env reports cookieSource=env.
func TestHandleEnv_EnvFallbackReportsEnvSource(t *testing.T) {
	resetServerConfig()
	t.Setenv("YUANBAO_COOKIE", "env-cookie-value")

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/env", nil)
	HandleEnv(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d", w.Code)
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["cookieSource"] != "env" {
		t.Errorf("cookieSource: got %v want env", resp["cookieSource"])
	}
}

// TestHandleEnv_NoCookieReportsNone verifies the "none" case.
func TestHandleEnv_NoCookieReportsNone(t *testing.T) {
	resetServerConfig()
	os.Unsetenv("YUANBAO_COOKIE")

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/env", nil)
	HandleEnv(c)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["cookieSource"] != "none" {
		t.Errorf("cookieSource: got %v want none", resp["cookieSource"])
	}
}

// TestHandleEnv_RuntimeCookieWins confirms a persisted runtime cookie
// is reported as cookieSource=runtime.
func TestHandleEnv_RuntimeCookieWins(t *testing.T) {
	resetServerConfig()
	os.Unsetenv("YUANBAO_COOKIE")

	serverConfigLock.Lock()
	serverConfig.YuanbaoCookie = cookiePointer("rt-tok", "rt-usr")
	serverConfigLock.Unlock()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/env", nil)
	HandleEnv(c)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["cookieSource"] != "runtime" {
		t.Errorf("cookieSource: got %v want runtime", resp["cookieSource"])
	}
}
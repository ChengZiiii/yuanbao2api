package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestHandleStatus_MultiProvider confirms /api/status returns a
// providers map keyed by provider name and top-level fields taken
// from the default provider's stats.
func TestHandleStatus_MultiProvider(t *testing.T) {
	resetServerConfig()
	t.Setenv("RUNTIME_CONFIG_PATH", t.TempDir()+"/absent.json")
	InitLimiterManager()

	// Seed serverConfig.Providers with two entries; defaultProvider
	// stays "yuanbao".
	enabled := true
	serverConfigLock.Lock()
	serverConfig.Providers = map[string]ProviderConfig{
		"yuanbao": {Enabled: &enabled, MaxConcurrency: intPointer(2)},
		"qwen":    {Enabled: &enabled, MaxConcurrency: intPointer(3)},
	}
	serverConfig.DefaultProvider = "yuanbao"
	serverConfigLock.Unlock()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/status", nil)
	HandleStatus(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status: %d, body=%s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	providers, ok := resp["providers"].(map[string]interface{})
	if !ok {
		t.Fatalf("providers missing or wrong type: %T", resp["providers"])
	}
	for _, name := range []string{"yuanbao", "qwen", "kimi"} {
		if _, ok := providers[name]; !ok {
			t.Errorf("providers.%s missing from status response", name)
		}
	}
	// Top-level fields must mirror yuanbao (the default provider).
	yuanbao, _ := providers["yuanbao"].(map[string]interface{})
	if int(yuanbao["maxConcurrency"].(float64)) != 2 {
		t.Errorf("yuanbao maxC: %v", yuanbao["maxConcurrency"])
	}
	if int(resp["maxConcurrency"].(float64)) != 2 {
		t.Errorf("top-level maxC: %v", resp["maxConcurrency"])
	}
}

// TestHandleStatus_TopLevelFollowsDefaultProvider verifies that the
// top-level fields switch when DefaultProvider changes.
func TestHandleStatus_TopLevelFollowsDefaultProvider(t *testing.T) {
	resetServerConfig()
	t.Setenv("RUNTIME_CONFIG_PATH", t.TempDir()+"/absent.json")
	InitLimiterManager()

	enabled := true
	serverConfigLock.Lock()
	serverConfig.Providers = map[string]ProviderConfig{
		"yuanbao": {Enabled: &enabled, MaxConcurrency: intPointer(1)},
		"qwen":    {Enabled: &enabled, MaxConcurrency: intPointer(5)},
	}
	serverConfig.DefaultProvider = "qwen"
	serverConfigLock.Unlock()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/status", nil)
	HandleStatus(c)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)

	if int(resp["maxConcurrency"].(float64)) != 5 {
		t.Errorf("top-level maxC should follow qwen (default), got %v", resp["maxConcurrency"])
	}
}
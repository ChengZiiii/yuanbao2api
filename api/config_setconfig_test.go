package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
)

// withRequest is a tiny helper that builds a gin test context with a
// JSON body and the standard Content-Type header. It is repeated
// enough below to be worth extracting.
func withRequest(body string) (*httptest.ResponseRecorder, *gin.Context) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/config", nil)
	c.Request.Body = &readCloser{data: body}
	c.Request.Header.Set("Content-Type", "application/json")
	return w, c
}

// TestHandleSetConfig_NewFormSavesYuanbaoCookie covers the new-shape
// happy path: POST {providers:{yuanbao:{cookie:{hyToken,hyUser}}}}
// must end up persisted as Providers["yuanbao"].Cookie with the same
// field values.
func TestHandleSetConfig_NewFormSavesYuanbaoCookie(t *testing.T) {
	resetServerConfig()
	t.Setenv("RUNTIME_CONFIG_PATH", filepath.Join(t.TempDir(), "rc.json"))

	body := `{"providers":{"yuanbao":{"enabled":true,"cookie":{"hyToken":"abc12345secret","hyUser":"user-xyz"}}}}`
	w, c := withRequest(body)
	HandleSetConfig(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, body=%s", w.Code, w.Body.String())
	}

	cfg := GetServerConfig()
	if cfg.YuanbaoCookie == nil {
		t.Fatalf("YuanbaoCookie should be set")
	}
	if cfg.YuanbaoCookie.HyToken != "abc12345secret" || cfg.YuanbaoCookie.HyUser != "user-xyz" {
		t.Errorf("cookie: got %+v", cfg.YuanbaoCookie)
	}
	loaded := LoadRuntimeConfig()
	if loaded.Providers["yuanbao"].Cookie == nil {
		t.Fatalf("persisted Providers[yuanbao].Cookie missing")
	}
	if loaded.Providers["yuanbao"].Cookie.HyToken != "abc12345secret" {
		t.Errorf("persisted cookie: %+v", loaded.Providers["yuanbao"].Cookie)
	}
}

// TestHandleSetConfig_LegacyFormTranslated confirms the dual-form
// behavior: a body with no `providers` key but with `yuanbaoCookie`
// must be translated to Providers["yuanbao"] and persisted in the new
// shape.
func TestHandleSetConfig_LegacyFormTranslated(t *testing.T) {
	resetServerConfig()
	t.Setenv("RUNTIME_CONFIG_PATH", filepath.Join(t.TempDir(), "rc.json"))

	body := `{"yuanbaoCookie":{"hyToken":"legacy-tok","hyUser":"legacy-usr"}}`
	w, c := withRequest(body)
	HandleSetConfig(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, body=%s", w.Code, w.Body.String())
	}

	loaded := LoadRuntimeConfig()
	if loaded.Providers["yuanbao"].Cookie == nil {
		t.Fatalf("legacy request must populate Providers[yuanbao].Cookie")
	}
	if loaded.Providers["yuanbao"].Cookie.HyToken != "legacy-tok" {
		t.Errorf("cookie: %+v", loaded.Providers["yuanbao"].Cookie)
	}
	if loaded.DefaultProvider != "yuanbao" {
		t.Errorf("DefaultProvider: got %q want yuanbao", loaded.DefaultProvider)
	}
}

// TestHandleSetConfig_LegacyFormConcurrency covers legacy
// maxConcurrency / queueTimeoutSeconds / requestCooldownMs being
// translated into the yuanbao provider block.
func TestHandleSetConfig_LegacyFormConcurrency(t *testing.T) {
	resetServerConfig()
	t.Setenv("RUNTIME_CONFIG_PATH", filepath.Join(t.TempDir(), "rc.json"))

	body := `{"maxConcurrency":7,"queueTimeoutSeconds":80,"requestCooldownMs":400}`
	w, c := withRequest(body)
	HandleSetConfig(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, body=%s", w.Code, w.Body.String())
	}

	loaded := LoadRuntimeConfig()
	yuanbao := loaded.Providers["yuanbao"]
	if yuanbao.MaxConcurrency == nil || *yuanbao.MaxConcurrency != 7 {
		t.Errorf("MaxConcurrency: %v", yuanbao.MaxConcurrency)
	}
	if yuanbao.QueueTimeoutSeconds == nil || *yuanbao.QueueTimeoutSeconds != 80 {
		t.Errorf("QueueTimeoutSeconds: %v", yuanbao.QueueTimeoutSeconds)
	}
	if yuanbao.RequestCooldownMs == nil || *yuanbao.RequestCooldownMs != 400 {
		t.Errorf("RequestCooldownMs: %v", yuanbao.RequestCooldownMs)
	}
}

// TestHandleSetConfig_NewFormRejectsNonObjectCookie makes sure the
// validation surface (cookie must be an object) is preserved on the
// new-form path.
func TestHandleSetConfig_NewFormRejectsNonObjectCookie(t *testing.T) {
	resetServerConfig()
	t.Setenv("RUNTIME_CONFIG_PATH", filepath.Join(t.TempDir(), "rc.json"))

	w, c := withRequest(`{"providers":{"yuanbao":{"cookie":"not-an-object"}}}`)
	HandleSetConfig(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// TestHandleSetConfig_NewFormRejectsUnknownField keeps the wire format
// strict: any unknown key inside a provider block yields a 400.
func TestHandleSetConfig_NewFormRejectsUnknownField(t *testing.T) {
	resetServerConfig()
	t.Setenv("RUNTIME_CONFIG_PATH", filepath.Join(t.TempDir(), "rc.json"))

	w, c := withRequest(`{"providers":{"yuanbao":{"enabled":true,"mystery":1}}}`)
	HandleSetConfig(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// TestHandleSetConfig_FlatFormIsNoOpForProviders verifies that an
// empty / flat-only body does not wipe out the persisted provider
// blocks.
func TestHandleSetConfig_FlatFormIsNoOpForProviders(t *testing.T) {
	resetServerConfig()
	t.Setenv("RUNTIME_CONFIG_PATH", filepath.Join(t.TempDir(), "rc.json"))

	// Seed a runtime cookie.
	seed := cookiePointer("seed-tok", "seed-usr")
	enabled := true
	if err := SaveRuntimeConfig(RuntimeConfig{
		Providers:       map[string]ProviderConfig{"yuanbao": {Enabled: &enabled, Cookie: seed}},
		DefaultProvider: "yuanbao",
	}); err != nil {
		t.Fatalf("seed save failed: %v", err)
	}
	serverConfigLock.Lock()
	serverConfig.Providers = map[string]ProviderConfig{"yuanbao": {Enabled: &enabled, Cookie: seed}}
	serverConfig.YuanbaoCookie = seed
	serverConfigLock.Unlock()

	// POST a flat body — only deepThinking flips.
	w, c := withRequest(`{"deepThinking":true}`)
	HandleSetConfig(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, body=%s", w.Code, w.Body.String())
	}
	cfg := GetServerConfig()
	if !cfg.DeepThinking {
		t.Errorf("DeepThinking: expected true, got %v", cfg.DeepThinking)
	}
	if cfg.YuanbaoCookie == nil || cfg.YuanbaoCookie.HyToken != "seed-tok" {
		t.Errorf("cookie should be preserved, got %+v", cfg.YuanbaoCookie)
	}
	loaded := LoadRuntimeConfig()
	if loaded.Providers["yuanbao"].Cookie == nil ||
		loaded.Providers["yuanbao"].Cookie.HyToken != "seed-tok" {
		t.Errorf("persisted cookie: %+v", loaded.Providers["yuanbao"].Cookie)
	}
}

// TestHandleSetConfig_DefaultProviderHonoured checks that the
// `defaultProvider` top-level field is persisted alongside providers.
func TestHandleSetConfig_DefaultProviderHonoured(t *testing.T) {
	resetServerConfig()
	t.Setenv("RUNTIME_CONFIG_PATH", filepath.Join(t.TempDir(), "rc.json"))

	body := `{"providers":{"yuanbao":{"enabled":true}},"defaultProvider":"yuanbao"}`
	w, c := withRequest(body)
	HandleSetConfig(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, body=%s", w.Code, w.Body.String())
	}
	loaded := LoadRuntimeConfig()
	if loaded.DefaultProvider != "yuanbao" {
		t.Errorf("DefaultProvider: got %q want yuanbao", loaded.DefaultProvider)
	}
}

// TestHandleSetConfig_DefaultProviderUnknownFails covers the case
// where defaultProvider refers to a provider not present in the
// providers[] block.
func TestHandleSetConfig_DefaultProviderUnknownFails(t *testing.T) {
	resetServerConfig()
	t.Setenv("RUNTIME_CONFIG_PATH", filepath.Join(t.TempDir(), "rc.json"))

	body := `{"providers":{"yuanbao":{"enabled":true}},"defaultProvider":"unknown"}`
	w, c := withRequest(body)
	HandleSetConfig(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// TestHandleSetConfig_ResponseShapeMatchesGetConfig verifies the
// response body contains the expected fields after a new-shape save.
func TestHandleSetConfig_ResponseShapeMatchesGetConfig(t *testing.T) {
	resetServerConfig()
	t.Setenv("RUNTIME_CONFIG_PATH", filepath.Join(t.TempDir(), "rc.json"))

	body := `{"providers":{"yuanbao":{"enabled":true,"cookie":{"hyToken":"t","hyUser":"u"}}},"defaultProvider":"yuanbao"}`
	w, c := withRequest(body)
	HandleSetConfig(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, body=%s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["defaultProvider"] != "yuanbao" {
		t.Errorf("response defaultProvider: %v", resp["defaultProvider"])
	}
	prov, ok := resp["providers"].(map[string]interface{})
	if !ok {
		t.Fatalf("response providers missing or wrong type: %T", resp["providers"])
	}
	yuanbao, ok := prov["yuanbao"].(map[string]interface{})
	if !ok {
		t.Fatalf("response providers.yuanbao missing")
	}
	cookie, ok := yuanbao["cookie"].(map[string]interface{})
	if !ok {
		t.Fatalf("response cookie missing or wrong type")
	}
	if cookie["hyToken"] != "t" {
		t.Errorf("response cookie.hyToken: %v", cookie["hyToken"])
	}
}
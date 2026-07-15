package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
)

// resetServerConfig restores the package-level serverConfig to its zero-ish state.
func resetServerConfig() {
	serverConfigLock.Lock()
	defer serverConfigLock.Unlock()
	serverConfig = &ServerConfigData{
		DeepThinking:        false,
		InternetSearch:      false,
		DefaultModel:        "deep_seek_v3",
		MaxConcurrency:      0,
		QueueTimeoutSeconds: 0,
		RequestCooldownMs:   0,
		AgentID:             "",
		YuanbaoCookie:       nil,
	}
}

func TestSyncAgentID_FromEnv(t *testing.T) {
	resetServerConfig()

	// Set env var
	os.Setenv("YUANBAO_AGENT_ID", "test-agent-from-env")
	defer os.Unsetenv("YUANBAO_AGENT_ID")

	SyncAgentID()

	cfg := GetServerConfig()
	if cfg.AgentID != "test-agent-from-env" {
		t.Errorf("expected AgentID='test-agent-from-env', got '%s'", cfg.AgentID)
	}
}

func TestSyncAgentID_SkipsWhenAlreadySet(t *testing.T) {
	resetServerConfig()

	// Pre-set AgentID directly
	serverConfigLock.Lock()
	serverConfig.AgentID = "already-set"
	serverConfigLock.Unlock()

	// Set a different env var — SyncAgentID must NOT overwrite
	os.Setenv("YUANBAO_AGENT_ID", "should-not-overwrite")
	defer os.Unsetenv("YUANBAO_AGENT_ID")

	SyncAgentID()

	cfg := GetServerConfig()
	if cfg.AgentID != "already-set" {
		t.Errorf("expected AgentID='already-set' (not overwritten), got '%s'", cfg.AgentID)
	}
}

func TestSyncAgentID_NoEnv(t *testing.T) {
	resetServerConfig()

	// Ensure env var is unset
	os.Unsetenv("YUANBAO_AGENT_ID")

	SyncAgentID()

	cfg := GetServerConfig()
	if cfg.AgentID != "" {
		t.Errorf("expected AgentID='' when no env and no preset, got '%s'", cfg.AgentID)
	}
}

func TestGetAgentID_ReadsConfigFirst(t *testing.T) {
	resetServerConfig()

	// Set config AgentID
	serverConfigLock.Lock()
	serverConfig.AgentID = "config-agent"
	serverConfigLock.Unlock()

	// Set different env — config must win
	os.Setenv("YUANBAO_AGENT_ID", "env-agent")
	defer os.Unsetenv("YUANBAO_AGENT_ID")

	agentID := getAgentID()
	if agentID != "config-agent" {
		t.Errorf("expected 'config-agent' (config wins), got '%s'", agentID)
	}
}

func TestGetAgentID_FallsBackToEnv(t *testing.T) {
	resetServerConfig()

	os.Setenv("YUANBAO_AGENT_ID", "env-agent")
	defer os.Unsetenv("YUANBAO_AGENT_ID")

	agentID := getAgentID()
	if agentID != "env-agent" {
		t.Errorf("expected 'env-agent' (fallback to env), got '%s'", agentID)
	}
}

func TestGetAgentID_FallsBackToDefault(t *testing.T) {
	resetServerConfig()

	os.Unsetenv("YUANBAO_AGENT_ID")

	agentID := getAgentID()
	if agentID != "naQivTmsDa" {
		t.Errorf("expected default 'naQivTmsDa', got '%s'", agentID)
	}
}

func TestServerConfigData_AgentIDField(t *testing.T) {
	resetServerConfig()

	serverConfigLock.Lock()
	serverConfig.AgentID = "my-agent"
	serverConfigLock.Unlock()

	data, err := json.Marshal(GetServerConfig())
	if err != nil {
		t.Fatalf("failed to marshal config: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if v, ok := decoded["agentId"]; !ok {
		t.Errorf("expected 'agentId' key in JSON output")
	} else if v != "my-agent" {
		t.Errorf("expected agentId='my-agent', got '%v'", v)
	}
}

func TestHandleSetConfig_AcceptsAgentID(t *testing.T) {
	resetServerConfig()

	body := `{"agentId":"runtime-agent"}`
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/config", nil)
	c.Request.Body = httptest.NewRequest("POST", "/api/config", nil).Body
	// We need to set the body properly
	c.Request = httptest.NewRequest("POST", "/api/config", nil)
	// Use a proper body
	bodyReader := &readCloser{data: body}
	c.Request.Body = bodyReader
	c.Request.Header.Set("Content-Type", "application/json")

	HandleSetConfig(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d. body=%s", w.Code, w.Body.String())
	}

	// Verify serverConfig was updated
	cfg := GetServerConfig()
	if cfg.AgentID != "runtime-agent" {
		t.Errorf("expected AgentID='runtime-agent', got '%s'", cfg.AgentID)
	}
}

// readCloser is a simple io.ReadCloser for test request bodies.
type readCloser struct {
	data string
	pos  int
}

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
	yuanbao, ok := loaded.Providers["yuanbao"]
	if !ok {
		t.Fatalf("Providers[yuanbao] missing after save")
	}
	if yuanbao.MaxConcurrency == nil || *yuanbao.MaxConcurrency != 3 ||
		yuanbao.QueueTimeoutSeconds == nil || *yuanbao.QueueTimeoutSeconds != 80 ||
		yuanbao.RequestCooldownMs == nil || *yuanbao.RequestCooldownMs != 400 {
		t.Errorf("runtime_config.json not persisted correctly: %+v", loaded)
	}
}

func TestHandleSetConfig_PersistenceFailureReturns500(t *testing.T) {
	resetServerConfig()
	t.Setenv("RUNTIME_CONFIG_PATH", filepath.Join(t.TempDir(), "missing", "runtime_config.json"))

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/config", &readCloser{data: `{"maxConcurrency":3}`})
	c.Request.Header.Set("Content-Type", "application/json")

	HandleSetConfig(c)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when persistence fails, got %d. body=%s", w.Code, w.Body.String())
	}
	var response map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if response["error"] == nil || response["error"] == "" {
		t.Errorf("expected persistence error details, got %s", w.Body.String())
	}
}

func TestHandleSetConfig_RejectsInvalidNumericValues(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "fractional max concurrency", body: `{"maxConcurrency":1.9}`},
		{name: "max concurrency below range", body: `{"maxConcurrency":0}`},
		{name: "max concurrency above range", body: `{"maxConcurrency":1001}`},
		{name: "max concurrency string", body: `{"maxConcurrency":"2"}`},
		{name: "fractional queue timeout", body: `{"queueTimeoutSeconds":1.5}`},
		{name: "queue timeout below range", body: `{"queueTimeoutSeconds":0}`},
		{name: "queue timeout above range", body: `{"queueTimeoutSeconds":3601}`},
		{name: "queue timeout boolean", body: `{"queueTimeoutSeconds":true}`},
		{name: "fractional cooldown", body: `{"requestCooldownMs":0.5}`},
		{name: "cooldown below range", body: `{"requestCooldownMs":-1}`},
		{name: "cooldown above range", body: `{"requestCooldownMs":60001}`},
		{name: "cooldown string", body: `{"requestCooldownMs":"0"}`},
		{name: "oversized number", body: `{"maxConcurrency":1e100}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetServerConfig()
			t.Setenv("RUNTIME_CONFIG_PATH", filepath.Join(t.TempDir(), "runtime_config.json"))

			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("POST", "/api/config", &readCloser{data: tt.body})
			c.Request.Header.Set("Content-Type", "application/json")

			HandleSetConfig(c)

			if w.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d. body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestHandleSetConfig_SavesYuanbaoCookie(t *testing.T) {
	resetServerConfig()
	path := filepath.Join(t.TempDir(), "rc.json")
	t.Setenv("RUNTIME_CONFIG_PATH", path)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/config", &readCloser{data: `{"yuanbaoCookie":{"hyToken":"abc12345supersecret","hyUser":"user-xyz"}}`})
	c.Request.Header.Set("Content-Type", "application/json")

	HandleSetConfig(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d. body=%s", w.Code, w.Body.String())
	}

	cfg := GetServerConfig()
	if cfg.YuanbaoCookie == nil {
		t.Fatalf("YuanbaoCookie should not be nil after save")
	}
	if cfg.YuanbaoCookie.HyToken != "abc12345supersecret" || cfg.YuanbaoCookie.HyUser != "user-xyz" {
		t.Errorf("YuanbaoCookie: got %+v, want {abc12345supersecret user-xyz}", cfg.YuanbaoCookie)
	}

	loaded := LoadRuntimeConfig()
	if loaded.Providers["yuanbao"].Cookie == nil {
		t.Fatalf("persisted YuanbaoCookie: got nil")
	}
	if loaded.Providers["yuanbao"].Cookie.HyToken != "abc12345supersecret" || loaded.Providers["yuanbao"].Cookie.HyUser != "user-xyz" {
		t.Errorf("persisted YuanbaoCookie: got %+v, want {abc12345supersecret user-xyz}", loaded.Providers["yuanbao"].Cookie)
	}
}

func TestHandleSetConfig_EmptyYuanbaoCookieClearsRuntimeOverride(t *testing.T) {
	resetServerConfig()
	path := filepath.Join(t.TempDir(), "rc.json")
	t.Setenv("RUNTIME_CONFIG_PATH", path)

	// Seed a runtime cookie.
	serverConfigLock.Lock()
	serverConfig.YuanbaoCookie = cookiePointer("preserved-token", "preserved-user")
	serverConfigLock.Unlock()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/config", &readCloser{data: `{"yuanbaoCookie":{}}`})
	c.Request.Header.Set("Content-Type", "application/json")

	HandleSetConfig(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d. body=%s", w.Code, w.Body.String())
	}

	cfg := GetServerConfig()
	if cfg.YuanbaoCookie != nil {
		t.Errorf("YuanbaoCookie: expected nil after clear, got %+v", cfg.YuanbaoCookie)
	}

	// Reload the persisted file: the YuanbaoCookie key must round-trip as
	// nil. With the pointer+omitempty tag, that means the key is absent
	// from the JSON.
	loaded := LoadRuntimeConfig()
	if loaded.Providers["yuanbao"].Cookie != nil {
		t.Errorf("persisted YuanbaoCookie: expected nil after clear, got %+v", loaded.Providers["yuanbao"].Cookie)
	}

	// EffectiveYuanbaoCookie should now fall back to env (or "" if unset).
	os.Unsetenv("YUANBAO_COOKIE")
	if got := EffectiveYuanbaoCookie(); got != "" {
		t.Errorf("EffectiveYuanbaoCookie after clear: got %q, want \"\"", got)
	}
}

func TestHandleSetConfig_OmittedYuanbaoCookieIsNoOp(t *testing.T) {
	resetServerConfig()
	path := filepath.Join(t.TempDir(), "rc.json")
	t.Setenv("RUNTIME_CONFIG_PATH", path)

	// Seed a runtime cookie both in memory and on disk.
	seed := cookiePointer("do-not-touch-tok", "do-not-touch-usr")
	enabled := true
	if err := SaveRuntimeConfig(RuntimeConfig{
		Providers: map[string]ProviderConfig{
			"yuanbao": {Enabled: &enabled, Cookie: seed},
		},
		DefaultProvider: "yuanbao",
	}); err != nil {
		t.Fatalf("failed to seed runtime config: %v", err)
	}
	serverConfigLock.Lock()
	serverConfig.YuanbaoCookie = seed
	serverConfigLock.Unlock()

	// Sanity: the file actually has the seeded cookie.
	if loaded := LoadRuntimeConfig(); loaded.Providers["yuanbao"].Cookie == nil ||
		loaded.Providers["yuanbao"].Cookie.HyToken != seed.HyToken || loaded.Providers["yuanbao"].Cookie.HyUser != seed.HyUser {
		t.Fatalf("seed failed: got %+v", loaded.Providers["yuanbao"].Cookie)
	}

	// Request without yuanbaoCookie but with an unrelated change.
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/config", &readCloser{data: `{"deepThinking":true}`})
	c.Request.Header.Set("Content-Type", "application/json")

	HandleSetConfig(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d. body=%s", w.Code, w.Body.String())
	}

	cfg := GetServerConfig()
	if cfg.YuanbaoCookie == nil ||
		cfg.YuanbaoCookie.HyToken != seed.HyToken || cfg.YuanbaoCookie.HyUser != seed.HyUser {
		t.Errorf("YuanbaoCookie: expected preserved %+v, got %+v", seed, cfg.YuanbaoCookie)
	}
	if !cfg.DeepThinking {
		t.Errorf("DeepThinking: expected true after partial update, got %v", cfg.DeepThinking)
	}

	// The spec scenario explicitly forbids rewriting the file in a way that
	// would clear the cookie. Reload and confirm the cookie is still there.
	loaded := LoadRuntimeConfig()
	if loaded.Providers["yuanbao"].Cookie == nil ||
		loaded.Providers["yuanbao"].Cookie.HyToken != seed.HyToken || loaded.Providers["yuanbao"].Cookie.HyUser != seed.HyUser {
		t.Errorf("persisted YuanbaoCookie: expected preserved %+v, got %+v", seed, loaded.Providers["yuanbao"].Cookie)
	}
}

func TestHandleSetConfig_RejectsNonStringYuanbaoCookie(t *testing.T) {
	resetServerConfig()
	t.Setenv("RUNTIME_CONFIG_PATH", filepath.Join(t.TempDir(), "rc.json"))

	// Numeric field inside the object is the primary new failure mode.
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/config", &readCloser{data: `{"yuanbaoCookie":{"hyToken":123}}`})
	c.Request.Header.Set("Content-Type", "application/json")

	HandleSetConfig(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d. body=%s", w.Code, w.Body.String())
	}

	cfg := GetServerConfig()
	if cfg.YuanbaoCookie != nil {
		t.Errorf("YuanbaoCookie should remain nil after rejected request, got %+v", cfg.YuanbaoCookie)
	}
}

func TestHandleSetConfig_RejectsNonObjectYuanbaoCookie(t *testing.T) {
	resetServerConfig()
	t.Setenv("RUNTIME_CONFIG_PATH", filepath.Join(t.TempDir(), "rc.json"))

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/config", &readCloser{data: `{"yuanbaoCookie":"not-an-object"}`})
	c.Request.Header.Set("Content-Type", "application/json")

	HandleSetConfig(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d. body=%s", w.Code, w.Body.String())
	}

	cfg := GetServerConfig()
	if cfg.YuanbaoCookie != nil {
		t.Errorf("YuanbaoCookie should remain nil after rejected request, got %+v", cfg.YuanbaoCookie)
	}
}

func TestHandleSetConfig_RejectsUnknownYuanbaoCookieField(t *testing.T) {
	resetServerConfig()
	t.Setenv("RUNTIME_CONFIG_PATH", filepath.Join(t.TempDir(), "rc.json"))

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/config", &readCloser{data: `{"yuanbaoCookie":{"hyToken":"a","extra":1}}`})
	c.Request.Header.Set("Content-Type", "application/json")

	HandleSetConfig(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for unknown field, got %d. body=%s", w.Code, w.Body.String())
	}
}

func TestYuanbaoCookie_HeaderValue(t *testing.T) {
	cases := []struct {
		name string
		in   YuanbaoCookie
		want string
	}{
		{name: "both fields", in: YuanbaoCookie{HyToken: "abc", HyUser: "xyz"}, want: "hy_token=abc; hy_user=xyz"},
		{name: "only token", in: YuanbaoCookie{HyToken: "abc", HyUser: ""}, want: "hy_token=abc"},
		{name: "only user", in: YuanbaoCookie{HyToken: "", HyUser: "xyz"}, want: "hy_user=xyz"},
		{name: "both empty", in: YuanbaoCookie{HyToken: "", HyUser: ""}, want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.in.HeaderValue(); got != tc.want {
				t.Errorf("HeaderValue(): got %q, want %q", got, tc.want)
			}
		})
	}

	// Nil receiver must also produce "" so callers can avoid nil-checks.
	var nilCookie *YuanbaoCookie
	if got := nilCookie.HeaderValue(); got != "" {
		t.Errorf("nil receiver HeaderValue(): got %q, want \"\"", got)
	}
}

func TestEffectiveYuanbaoCookie_StructureBased(t *testing.T) {
	resetServerConfig()
	os.Unsetenv("YUANBAO_COOKIE")

	// Neither: returns "".
	if got := EffectiveYuanbaoCookie(); got != "" {
		t.Errorf("neither set: got %q, want \"\"", got)
	}
	if got := EffectiveYuanbaoCookieSource(); got != CookieSourceNone {
		t.Errorf("neither set source: got %q, want %q", got, CookieSourceNone)
	}

	// Env only.
	t.Setenv("YUANBAO_COOKIE", "env-cookie")
	if got := EffectiveYuanbaoCookie(); got != "env-cookie" {
		t.Errorf("env only: got %q, want %q", got, "env-cookie")
	}
	if got := EffectiveYuanbaoCookieSource(); got != CookieSourceEnv {
		t.Errorf("env only source: got %q, want %q", got, CookieSourceEnv)
	}

	// Runtime struct with both fields overrides env.
	serverConfigLock.Lock()
	serverConfig.YuanbaoCookie = cookiePointer("runtime-tok", "runtime-usr")
	serverConfigLock.Unlock()
	want := "hy_token=runtime-tok; hy_user=runtime-usr"
	if got := EffectiveYuanbaoCookie(); got != want {
		t.Errorf("runtime both fields: got %q, want %q", got, want)
	}
	if got := EffectiveYuanbaoCookieSource(); got != CookieSourceRuntime {
		t.Errorf("runtime source: got %q, want %q", got, CookieSourceRuntime)
	}

	// Runtime with only token still wins over env.
	serverConfigLock.Lock()
	serverConfig.YuanbaoCookie = cookiePointer("only-token", "")
	serverConfigLock.Unlock()
	if got := EffectiveYuanbaoCookie(); got != "hy_token=only-token" {
		t.Errorf("runtime token-only: got %q, want %q", got, "hy_token=only-token")
	}
	if got := EffectiveYuanbaoCookieSource(); got != CookieSourceRuntime {
		t.Errorf("runtime token-only source: got %q, want %q", got, CookieSourceRuntime)
	}

	// All-empty struct falls back to env.
	serverConfigLock.Lock()
	serverConfig.YuanbaoCookie = &YuanbaoCookie{}
	serverConfigLock.Unlock()
	if got := EffectiveYuanbaoCookie(); got != "env-cookie" {
		t.Errorf("all-empty runtime falls back: got %q, want %q", got, "env-cookie")
	}
	if got := EffectiveYuanbaoCookieSource(); got != CookieSourceEnv {
		t.Errorf("all-empty runtime source: got %q, want %q", got, CookieSourceEnv)
	}

	// Nil runtime also falls back.
	serverConfigLock.Lock()
	serverConfig.YuanbaoCookie = nil
	serverConfigLock.Unlock()
	if got := EffectiveYuanbaoCookie(); got != "env-cookie" {
		t.Errorf("nil runtime falls back: got %q, want %q", got, "env-cookie")
	}
}

func TestEffectiveYuanbaoCookie_Priority(t *testing.T) {
	resetServerConfig()
	os.Unsetenv("YUANBAO_COOKIE")

	// Neither: returns ""
	if got := EffectiveYuanbaoCookie(); got != "" {
		t.Errorf("neither set: got %q, want \"\"", got)
	}
	if got := EffectiveYuanbaoCookieSource(); got != CookieSourceNone {
		t.Errorf("neither set source: got %q, want %q", got, CookieSourceNone)
	}

	// Env only.
	t.Setenv("YUANBAO_COOKIE", "env-cookie")
	if got := EffectiveYuanbaoCookie(); got != "env-cookie" {
		t.Errorf("env only: got %q, want %q", got, "env-cookie")
	}
	if got := EffectiveYuanbaoCookieSource(); got != CookieSourceEnv {
		t.Errorf("env only source: got %q, want %q", got, CookieSourceEnv)
	}

	// Runtime overrides env.
	serverConfigLock.Lock()
	rt := "runtime-cookie"
	serverConfig.YuanbaoCookie = cookiePointer(rt, "")
	serverConfigLock.Unlock()
	if got := EffectiveYuanbaoCookie(); got != "hy_token="+rt {
		t.Errorf("runtime overrides env: got %q, want %q", got, "hy_token="+rt)
	}
	if got := EffectiveYuanbaoCookieSource(); got != CookieSourceRuntime {
		t.Errorf("runtime overrides env source: got %q, want %q", got, CookieSourceRuntime)
	}

	// Empty runtime falls back to env.
	serverConfigLock.Lock()
	serverConfig.YuanbaoCookie = &YuanbaoCookie{}
	serverConfigLock.Unlock()
	if got := EffectiveYuanbaoCookie(); got != "env-cookie" {
		t.Errorf("empty runtime falls back: got %q, want %q", got, "env-cookie")
	}
}

func (r *readCloser) Read(p []byte) (n int, err error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n = copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

func (r *readCloser) Close() error {
	return nil
}

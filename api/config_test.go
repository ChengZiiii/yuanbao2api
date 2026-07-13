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
	if loaded.MaxConcurrency == nil || *loaded.MaxConcurrency != 3 ||
		loaded.QueueTimeoutSeconds == nil || *loaded.QueueTimeoutSeconds != 80 ||
		loaded.RequestCooldownMs == nil || *loaded.RequestCooldownMs != 400 {
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

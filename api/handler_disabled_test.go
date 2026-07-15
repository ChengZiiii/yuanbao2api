package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"yuanbao2api/internal/models"
	providers "yuanbao2api/providers"
)

// TestHandleOpenAI_DisabledProviderRejected exercises the spec scenario
// "命中但 provider 停用" via the chat-completions handler: a model
// route to a known provider that is marked enabled=false in
// RuntimeConfig.Providers must yield HTTP 503 with the error message
// containing "provider disabled".
func TestHandleOpenAI_DisabledProviderRejected(t *testing.T) {
	resetServerConfig()
	t.Setenv("RUNTIME_CONFIG_PATH", filepath.Join(t.TempDir(), "rc.json"))

	reg := providers.NewRegistry()
	reg.Register(&minimalProvider{name: "yuanbao", modelID: "deep_seek_v3"})
	SetProviderRegistry(reg)
	t.Cleanup(func() { SetProviderRegistry(nil) })

	// Disable the yuanbao provider.
	disabled := false
	serverConfigLock.Lock()
	serverConfig.Providers = map[string]ProviderConfig{
		"yuanbao": {Enabled: &disabled},
	}
	serverConfigLock.Unlock()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body := `{"model":"deep_seek_v3","messages":[{"role":"user","content":"hi"}]}`
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	c.Request.Body = &readCloser{data: body}
	c.Request.Header.Set("Content-Type", "application/json")
	HandleOpenAIChatCompletion(c)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d want 503, body=%s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	errMsg, _ := resp["error"].(string)
	if errMsg == "" {
		t.Fatalf("expected error field, got %s", w.Body.String())
	}
	if !contains(errMsg, "provider disabled") {
		t.Errorf("error %q should contain 'provider disabled'", errMsg)
	}
}

// TestHandleAnthropic_DisabledProviderRejected mirrors the openai test
// for the Anthropic /v1/messages handler.
func TestHandleAnthropic_DisabledProviderRejected(t *testing.T) {
	resetServerConfig()
	t.Setenv("RUNTIME_CONFIG_PATH", filepath.Join(t.TempDir(), "rc.json"))

	reg := providers.NewRegistry()
	reg.Register(&minimalProvider{name: "yuanbao", modelID: "deep_seek_v3"})
	SetProviderRegistry(reg)
	t.Cleanup(func() { SetProviderRegistry(nil) })

	disabled := false
	serverConfigLock.Lock()
	serverConfig.Providers = map[string]ProviderConfig{
		"yuanbao": {Enabled: &disabled},
	}
	serverConfigLock.Unlock()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body := `{"model":"deep_seek_v3","max_tokens":64,"messages":[{"role":"user","content":"hi"}]}`
	c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	c.Request.Body = &readCloser{data: body}
	c.Request.Header.Set("Content-Type", "application/json")
	HandleAnthropicMessages(c)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d want 503, body=%s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	// Anthropic error envelope: {"type":"error","error":{"type":...,"message":...}}
	if msg, _ := resp["message"].(string); msg == "" {
		// Try the nested envelope.
		if inner, ok := resp["error"].(map[string]interface{}); ok {
			msg, _ = inner["message"].(string)
		}
		if msg == "" {
			t.Fatalf("expected error message, got %s", w.Body.String())
		}
		if !contains(msg, "provider disabled") {
			t.Errorf("error message %q should contain 'provider disabled'", msg)
		}
	}
}

// minimalProvider implements providers.Provider with just enough
// behaviour to make Route succeed. BuildPrompt / Send are never
// invoked because the handler rejects the request before reaching
// them when the provider is disabled.
type minimalProvider struct {
	name    string
	modelID string
}

func (m *minimalProvider) Name() string  { return m.name }
func (m *minimalProvider) Models() []providers.ModelInfo {
	return []providers.ModelInfo{{ID: m.modelID}}
}
func (m *minimalProvider) BuildPrompt(_ []providers.Message, _ []providers.Tool) (string, string, error) {
	return "", "", nil
}
func (m *minimalProvider) NewRequest(_ string, _ providers.RequestOptions) (any, error) {
	return nil, nil
}
func (m *minimalProvider) Send(_ any, _, _ string) (*http.Response, error) {
	return nil, nil
}
func (m *minimalProvider) ParseStreamLine(_ string) (*providers.StreamChunk, error) {
	return nil, nil
}

func contains(s, sub string) bool {
	if len(s) < len(sub) {
		return false
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// avoid unused-import warnings if models is removed during refactor
var _ = models.ResponseMessage{}
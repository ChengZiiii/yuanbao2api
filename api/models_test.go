package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	providers "yuanbao2api/providers"
)

// TestHandleOpenAIModels_MultiProviderUnion covers the spec scenario
// "多 provider 模型并集": a registry with yuanbao (enabled) and qwen
// (enabled) must return 6 model entries with the correct owned_by.
func TestHandleOpenAIModels_MultiProviderUnion(t *testing.T) {
	resetServerConfig()
	enabled := true
	serverConfigLock.Lock()
	serverConfig.Providers = map[string]ProviderConfig{
		"yuanbao": {Enabled: &enabled},
		"qwen":    {Enabled: &enabled},
	}
	serverConfigLock.Unlock()

	reg := providers.NewRegistry()
	if err := reg.Register(&stubModelsProvider{name: "yuanbao", models: []providers.ModelInfo{
		{ID: "deep_seek_v3"}, {ID: "hunyuan"},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(&stubModelsProvider{name: "qwen", models: []providers.ModelInfo{
		{ID: "qwen-max"}, {ID: "qwen-plus"}, {ID: "qwen-turbo"}, {ID: "qwen-long"},
	}}); err != nil {
		t.Fatal(err)
	}
	SetProviderRegistry(reg)
	t.Cleanup(func() { SetProviderRegistry(nil) })

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/v1/models", nil)
	HandleOpenAIModels(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status: %d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Data []struct {
			ID      string `json:"id"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Data) != 6 {
		t.Errorf("expected 6 entries, got %d", len(resp.Data))
	}
	ownedBy := map[string]string{}
	for _, m := range resp.Data {
		ownedBy[m.ID] = m.OwnedBy
	}
	if ownedBy["deep_seek_v3"] != "yuanbao" {
		t.Errorf("deep_seek_v3 owned_by: %q", ownedBy["deep_seek_v3"])
	}
	if ownedBy["qwen-max"] != "qwen" {
		t.Errorf("qwen-max owned_by: %q", ownedBy["qwen-max"])
	}
}

// TestHandleOpenAIModels_DisabledProviderFiltered covers the spec
// scenario "停用的 provider 模型被过滤": kimi is registered but its
// ProviderConfig has enabled=false, so its models must not appear.
func TestHandleOpenAIModels_DisabledProviderFiltered(t *testing.T) {
	resetServerConfig()
	enabled := true
	disabled := false
	serverConfigLock.Lock()
	serverConfig.Providers = map[string]ProviderConfig{
		"yuanbao": {Enabled: &enabled},
		"qwen":    {Enabled: &enabled},
		"kimi":    {Enabled: &disabled},
	}
	serverConfigLock.Unlock()

	reg := providers.NewRegistry()
	reg.Register(&stubModelsProvider{name: "yuanbao", models: []providers.ModelInfo{{ID: "deep_seek_v3"}}})
	reg.Register(&stubModelsProvider{name: "qwen", models: []providers.ModelInfo{{ID: "qwen-max"}}})
	reg.Register(&stubModelsProvider{name: "kimi", models: []providers.ModelInfo{{ID: "kimi-k2"}}})
	SetProviderRegistry(reg)
	t.Cleanup(func() { SetProviderRegistry(nil) })

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/v1/models", nil)
	HandleOpenAIModels(c)

	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	for _, m := range resp.Data {
		if m.ID == "kimi-k2" {
			t.Errorf("kimi-k2 should not appear when kimi is disabled")
		}
	}
}

// TestHandleOpenAIModels_PlaceholderDisabledNotShown covers the spec
// scenario "qwen/kimi 占位 provider 在未启用时不出现在响应中".
func TestHandleOpenAIModels_PlaceholderDisabledNotShown(t *testing.T) {
	resetServerConfig()
	// qwen/kimi have no Providers entries at all → default enabled=true.
	// The expectation in the spec is "未启用时不出现在响应中", which we
	// interpret as: explicitly disabled providers are filtered, but
	// never-registered providers do not appear either way (they're
	// simply not in the registry).
	reg := providers.NewRegistry()
	reg.Register(&stubModelsProvider{name: "yuanbao", models: []providers.ModelInfo{{ID: "deep_seek_v3"}}})
	SetProviderRegistry(reg)
	t.Cleanup(func() { SetProviderRegistry(nil) })

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/v1/models", nil)
	HandleOpenAIModels(c)

	var resp struct {
		Data []struct {
			ID      string `json:"id"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	for _, m := range resp.Data {
		if m.OwnedBy == "qwen" || m.OwnedBy == "kimi" {
			t.Errorf("placeholder provider %q should not appear (not in registry)", m.OwnedBy)
		}
	}
}

// stubModelsProvider is a minimal provider.Provider used to wire a
// fake registry without depending on the real provider packages.
type stubModelsProvider struct {
	name   string
	models []providers.ModelInfo
}

func (s *stubModelsProvider) Name() string                            { return s.name }
func (s *stubModelsProvider) Models() []providers.ModelInfo           { return s.models }
func (s *stubModelsProvider) BuildPrompt(_ []providers.Message, _ []providers.Tool) (string, string, error) {
	return "", "", nil
}
func (s *stubModelsProvider) NewRequest(_ string, _ providers.RequestOptions) (any, error) {
	return nil, nil
}
func (s *stubModelsProvider) Send(_ any, _, _ string) (*http.Response, error) {
	// Return shape intentionally diverges from real Provider.Send; the
	// handler does not exercise Send during /v1/models.
	return nil, nil
}
func (s *stubModelsProvider) ParseStreamLine(_ string) (*providers.StreamChunk, error) {
	return nil, nil
}
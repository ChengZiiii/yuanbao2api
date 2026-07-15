// Package qwen is a placeholder provider for the (planned) Alibaba
// Qwen web2api integration. It satisfies provider.Provider so the
// registry can route "qwen-*" model names, but every method that
// would actually talk to upstream returns "not implemented" so the
// caller gets a clear 501-style error.
package qwen

import (
	"net/http"

	providers "yuanbao2api/providers"
)

// Provider implements providers.Provider as a placeholder. The fields
// here are only kept so future real implementations have a stable
// shape to grow into; they are unused on the placeholder path.
type Provider struct {
	BaseURL  string
	Endpoint string
}

// New constructs a ready-to-register placeholder Provider.
func New() *Provider {
	return &Provider{
		BaseURL:  "https://qianwen.example",
		Endpoint: "/api/chat",
	}
}

// Name returns the provider identifier used by the registry.
func (p *Provider) Name() string { return "qwen" }

// Models advertises the official Qwen model names. Until the real
// adapter lands the registry still routes these names to this
// provider; the placeholder error is then returned by Send /
// BuildPrompt / NewRequest.
func (p *Provider) Models() []providers.ModelInfo {
	return []providers.ModelInfo{
		{ID: "qwen-max", DisplayName: "Qwen Max", Description: "通义千问 Max — 旗舰模型"},
		{ID: "qwen-plus", DisplayName: "Qwen Plus", Description: "通义千问 Plus — 增强模型"},
		{ID: "qwen-turbo", DisplayName: "Qwen Turbo", Description: "通义千问 Turbo — 高速模型"},
		{ID: "qwen-long", DisplayName: "Qwen Long", Description: "通义千问 Long — 长上下文"},
	}
}

// BuildPrompt is a placeholder.
func (p *Provider) BuildPrompt(_ []providers.Message, _ []providers.Tool) (string, string, error) {
	return "", "", ErrNotImplemented
}

// NewRequest is a placeholder.
func (p *Provider) NewRequest(_ string, _ providers.RequestOptions) (any, error) {
	return nil, ErrNotImplemented
}

// Send is a placeholder. It deliberately returns an error rather than
// attempting a network round trip so the handler surfaces a clear
// "not implemented" message instead of a generic 5xx.
func (p *Provider) Send(_ any, _, _ string) (*http.Response, error) {
	return nil, ErrNotImplemented
}

// ParseStreamLine is a placeholder. Always returns (nil, nil) so the
// handler skips over every line without errors; with Send refusing
// the request, ParseStreamLine should never actually be invoked on
// the happy flow.
func (p *Provider) ParseStreamLine(_ string) (*providers.StreamChunk, error) {
	return nil, nil
}

// ErrNotImplemented is the sentinel returned by every placeholder
// method. Callers can errors.Is against it to distinguish "provider
// wired but not yet implemented" from network / 4xx / 5xx errors.
var ErrNotImplemented = &notImplementedError{name: "qwen"}

type notImplementedError struct{ name string }

func (e *notImplementedError) Error() string {
	return e.name + " provider is not yet implemented"
}
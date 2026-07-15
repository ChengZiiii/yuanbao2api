// Package kimi is a placeholder provider for the (planned) Moonshot
// Kimi web2api integration. It mirrors providers/qwen so the registry
// can route kimi-* model names; all upstream-touching methods return
// ErrNotImplemented until the real adapter lands.
package kimi

import (
	"net/http"

	providers "yuanbao2api/providers"
)

// Provider implements providers.Provider as a placeholder.
type Provider struct {
	BaseURL  string
	Endpoint string
}

// New constructs a ready-to-register placeholder Provider.
func New() *Provider {
	return &Provider{
		BaseURL:  "https://kimi.example",
		Endpoint: "/api/chat",
	}
}

// Name returns the provider identifier used by the registry.
func (p *Provider) Name() string { return "kimi" }

// Models advertises the official Kimi / Moonshot model names.
func (p *Provider) Models() []providers.ModelInfo {
	return []providers.ModelInfo{
		{ID: "kimi-k2", DisplayName: "Kimi K2", Description: "Moonshot Kimi K2"},
		{ID: "moonshot-v1-128k", DisplayName: "Moonshot v1 128k", Description: "Moonshot v1 128k context"},
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

// Send is a placeholder.
func (p *Provider) Send(_ any, _, _ string) (*http.Response, error) {
	return nil, ErrNotImplemented
}

// ParseStreamLine is a placeholder. Always returns (nil, nil).
func (p *Provider) ParseStreamLine(_ string) (*providers.StreamChunk, error) {
	return nil, nil
}

// ErrNotImplemented is the sentinel returned by every placeholder
// method.
var ErrNotImplemented = &notImplementedError{name: "kimi"}

type notImplementedError struct{ name string }

func (e *notImplementedError) Error() string {
	return e.name + " provider is not yet implemented"
}
// Package kimi contains a placeholder HTTP client stub for the
// (planned) Moonshot Kimi integration. Currently unused; see
// provider.go for the placeholder Provider implementation.
package kimi

// Client is a placeholder HTTP client.
type Client struct {
	BaseURL  string
	Endpoint string
}

// NewClient returns an unconfigured placeholder client.
func NewClient() *Client {
	return &Client{
		BaseURL:  "https://kimi.example",
		Endpoint: "/api/chat",
	}
}
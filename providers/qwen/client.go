// Package qwen contains a placeholder HTTP client stub for the
// (planned) Qwen web2api integration. The real implementation will
// live here; today the file only documents the intended shape so the
// other provider packages can reference qwen.NewClient as a future
// extension point.
package qwen

// Client is a placeholder HTTP client. Fields are kept for future
// expansion but currently unused; the placeholder Provider does not
// instantiate one.
type Client struct {
	BaseURL  string
	Endpoint string
}

// NewClient returns an unconfigured placeholder client.
func NewClient() *Client {
	return &Client{
		BaseURL:  "https://qianwen.example",
		Endpoint: "/api/chat",
	}
}
// Package provider defines the abstract contract every upstream web2api
// adapter must satisfy, plus the shared stream-chunk shape and the request
// option types. Concrete adapters live in subpackages
// (providers/yuanbao, providers/qwen, providers/kimi) and register
// themselves with the global Registry at startup.
package provider

import "net/http"

// ModelInfo is the lightweight metadata a provider exposes about one of
// its supported models. The handler layer translates this into the
// OpenAI-compatible ModelInfo struct for the /v1/models response.
type ModelInfo struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName,omitempty"`
	Description string `json:"description,omitempty"`
}

// Message is the cross-provider message shape used during prompt
// construction. Role is "system" / "user" / "assistant" / "tool";
// Content may be a plain string or a slice of content blocks (Anthropic).
type Message struct {
	Role       string
	Content    interface{}
	Name       string
	ToolCalls  []ToolCall
	ToolCallID string
}

// Tool mirrors the OpenAI function-tool schema; provider implementations
// translate this into whatever the upstream protocol needs.
type Tool struct {
	Type     string
	Function ToolFunction
}

// ToolFunction describes one callable function inside a Tool.
type ToolFunction struct {
	Name        string
	Description string
	Parameters  interface{}
}

// ToolCall describes one assistant tool invocation. Provider adapters
// may consume or ignore this depending on native tool-call support.
type ToolCall struct {
	ID       string
	Type     string
	Function ToolFunctionCall
	Index    int
}

// ToolFunctionCall holds the serialized function name and arguments.
type ToolFunctionCall struct {
	Name      string
	Arguments string
}

// RequestOptions bundles the per-request flags a provider needs to
// decide on deep thinking, internet search, agent identity, etc.
type RequestOptions struct {
	Model           string
	UseDeepThinking bool
	UseInternet     bool
	AgentID         string
}

// StreamChunk is the uniform shape every provider's ParseStreamLine
// produces. Handlers consume these without caring which provider is
// upstream.
//
//	Type == "think" → Content holds the reasoning content.
//	Type == "text"  → Text holds the visible reply text.
//	Any other Type   → handlers should treat it as opaque and skip.
type StreamChunk struct {
	Type    string
	Content string
	Text    string
}

// Provider is the contract every upstream web2api must satisfy.
//
// Implementations are expected to be safe for concurrent use after
// construction; all per-request state is supplied through method
// arguments. The registry holds one instance per provider and routes
// requests to it based on the requested model name.
type Provider interface {
	// Name returns the provider identifier (e.g. "yuanbao", "qwen").
	Name() string

	// Models returns the list of models this provider advertises.
	Models() []ModelInfo

	// BuildPrompt renders the cross-provider messages + tools into the
	// provider's internal prompt representation. The second return value
	// is the tool-system-prompt fragment, when separate from the main
	// prompt.
	BuildPrompt(messages []Message, tools []Tool) (string, string, error)

	// NewRequest builds the provider-specific request body. The result
	// is opaque (any) — providers know their own types and Send will
	// assert the value back to the right concrete type.
	NewRequest(prompt string, opts RequestOptions) (any, error)

	// Send dispatches the request and returns the raw upstream
	// response. Implementations are responsible for translating
	// (req any) back into their concrete type. Errors cover network
	// failures, timeouts, and non-2xx upstream status codes.
	Send(req any, agentID, conversationID string) (*http.Response, error)

	// ParseStreamLine parses one SSE line into a uniform StreamChunk.
	// Returns (nil, nil) for empty / non-data / [DONE] lines so the
	// handler can skip them without branching on provider-specific
	// framing. Placeholder providers (qwen/kimi in this change) always
	// return (nil, nil) since they never reach this code path on the
	// happy flow.
	ParseStreamLine(line string) (*StreamChunk, error)
}
package yuanbao

import (
	"net/http"

	providers "yuanbao2api/providers"
)

// Provider implements providers.Provider for the Tencent Yuanbao
// upstream. Each method is a thin wrapper around the legacy
// Client/* helpers so the request / response framing stays in one
// place while the handler layer reasons about the uniform Provider
// interface.
type Provider struct {
	client *Client
}

// New constructs a ready-to-register Provider instance.
func New() *Provider {
	return &Provider{client: NewClient()}
}

// Name returns the provider identifier used by the registry.
func (p *Provider) Name() string { return "yuanbao" }

// Models enumerates the model IDs the registry may route to this
// provider. Aliases are listed separately so the registry's
// case-insensitive model lookup can resolve legacy names like
// "DeepSeek-V3.2" → "deep_seek_v3" routing without a second lookup
// table.
func (p *Provider) Models() []providers.ModelInfo {
	return []providers.ModelInfo{
		{ID: "deep_seek_v3", DisplayName: "DeepSeek V3.2", Description: "适合深度思考和复杂推理任务"},
		{ID: "hunyuan", DisplayName: "Hy3 preview", Description: "腾讯混元大模型，全能处理"},
		{ID: "deepseek", DisplayName: "DeepSeek V3.2", Description: "适合深度思考和复杂推理任务 (alias)"},
		{ID: "hunyuan-t1", DisplayName: "Hy3 preview (T1)", Description: "腾讯混元大模型 (alias)"},
		{ID: "gpt_175B_0404", DisplayName: "GPT 175B", Description: "元宝内部模型"},
	}
}

// BuildPrompt is implemented in prompt.go (delegated to the OpenAI/
// Anthropic-specific helpers so each style can evolve independently).
func (p *Provider) BuildPrompt(messages []providers.Message, tools []providers.Tool) (string, string, error) {
	return BuildPrompt(messages, tools)
}

// NewRequest constructs the provider-specific request body. The opts
// payload drives the chatModelID selection (deep thinking toggles
// hunyuan_t1 vs deep_seek_v3) and the supportFunctions list.
func (p *Provider) NewRequest(prompt string, opts providers.RequestOptions) (any, error) {
	return BuildRequest(prompt, opts)
}

// Send dispatches the upstream request. The req value MUST be a
// *YuanbaoRequest built by NewRequest; any other type is rejected
// with an error rather than panicking, so the handler can convert
// the failure into a clean 5xx response.
func (p *Provider) Send(req any, agentID, conversationID string) (*http.Response, error) {
	yr, ok := req.(*YuanbaoRequest)
	if !ok {
		return nil, ErrInvalidRequest
	}
	return p.client.SendRequestWithID(*yr, agentID, conversationID)
}

// ParseStreamLine translates a raw SSE line into the uniform
// StreamChunk. The underlying parser returns the yuanbao-internal
// YuanbaoResponseChunk; we remap Type=="text" → Text, Type=="think"
// → Content so handlers can consume the result without per-provider
// branching.
func (p *Provider) ParseStreamLine(line string) (*providers.StreamChunk, error) {
	chunk, err := ParseStreamLine(line)
	if err != nil || chunk == nil {
		return nil, err
	}
	sc := &providers.StreamChunk{Type: chunk.Type, Content: chunk.Content, Text: chunk.Msg}
	return sc, nil
}

// ErrInvalidRequest is returned by Send when the request payload was
// not produced by NewRequest (i.e. a type mismatch in handler code).
var ErrInvalidRequest = &invalidRequestError{}

type invalidRequestError struct{}

func (*invalidRequestError) Error() string {
	return "yuanbao provider: Send received request of unexpected type (use NewRequest)"
}
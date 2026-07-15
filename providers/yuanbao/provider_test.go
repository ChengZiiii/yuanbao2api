package yuanbao

import (
	"strings"
	"testing"

	providers "yuanbao2api/providers"
)

func TestProvider_NameAndModels(t *testing.T) {
	p := New()
	if p.Name() != "yuanbao" {
		t.Errorf("Name: got %q want yuanbao", p.Name())
	}
	models := p.Models()
	if len(models) == 0 {
		t.Fatal("Models() returned empty slice")
	}
	want := map[string]bool{
		"deep_seek_v3": false,
		"hunyuan":      false,
	}
	for _, m := range models {
		if _, ok := want[m.ID]; ok {
			want[m.ID] = true
		}
	}
	for id, found := range want {
		if !found {
			t.Errorf("Models() missing required id %q", id)
		}
	}
}

func TestProvider_BuildPromptIncludesUserAndAssistant(t *testing.T) {
	p := New()
	prompt, toolSys, err := p.BuildPrompt([]providers.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
		{Role: "user", Content: "how are you?"},
	}, nil)
	if err != nil {
		t.Fatalf("BuildPrompt: %v", err)
	}
	for _, want := range []string{"用户: hello", "助手: hi", "用户: how are you?", "请作为助手继续回复"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
	if toolSys != "" {
		t.Errorf("toolSys: expected empty when no tools, got %q", toolSys)
	}
}

func TestProvider_BuildPromptInjectsToolSystem(t *testing.T) {
	p := New()
	tools := []providers.Tool{{Type: "function", Function: providers.ToolFunction{Name: "f1", Description: "d1", Parameters: "{}"}}}
	prompt, toolSys, err := p.BuildPrompt([]providers.Message{
		{Role: "user", Content: "go"},
	}, tools)
	if err != nil {
		t.Fatalf("BuildPrompt: %v", err)
	}
	if toolSys == "" {
		t.Errorf("toolSys: expected non-empty")
	}
	if !strings.Contains(prompt, "工具") {
		t.Errorf("prompt should mention tools")
	}
}

func TestProvider_BuildRequest_DeepSeekDeepThinking(t *testing.T) {
	p := New()
	req, err := p.NewRequest("hello", providers.RequestOptions{
		Model:           "deep_seek_v3",
		UseDeepThinking: true,
		AgentID:         "aid",
	})
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	yr, ok := req.(*YuanbaoRequest)
	if !ok {
		t.Fatalf("NewRequest returned wrong type: %T", req)
	}
	if yr.ChatModelID != "deep_seek" {
		t.Errorf("ChatModelID with deep thinking: got %q want deep_seek", yr.ChatModelID)
	}
	if yr.Plugin != "" {
		t.Errorf("Plugin should be empty under deep thinking, got %q", yr.Plugin)
	}
	if yr.AgentID != "aid" {
		t.Errorf("AgentID: got %q want aid", yr.AgentID)
	}
}

func TestProvider_BuildRequest_HunyuanDeepThinking(t *testing.T) {
	p := New()
	req, err := p.NewRequest("hi", providers.RequestOptions{
		Model:           "hunyuan",
		UseDeepThinking: true,
		AgentID:         "aid",
	})
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	yr := req.(*YuanbaoRequest)
	if yr.ChatModelID != "hunyuan_t1" {
		t.Errorf("ChatModelID: got %q want hunyuan_t1", yr.ChatModelID)
	}
}

func TestProvider_BuildRequest_InternetSearch(t *testing.T) {
	p := New()
	req, _ := p.NewRequest("x", providers.RequestOptions{Model: "deep_seek_v3", UseInternet: true})
	yr := req.(*YuanbaoRequest)
	if len(yr.SupportFunctions) != 1 || yr.SupportFunctions[0] != "openInternetSearch" {
		t.Errorf("SupportFunctions: got %v", yr.SupportFunctions)
	}
}

func TestProvider_ParseStreamLine_ThinkChunk(t *testing.T) {
	p := New()
	sc, err := p.ParseStreamLine(`data: {"type":"think","content":"hmm..."}`)
	if err != nil {
		t.Fatalf("ParseStreamLine: %v", err)
	}
	if sc == nil {
		t.Fatal("expected chunk, got nil")
	}
	if sc.Type != "think" || sc.Content != "hmm..." {
		t.Errorf("think chunk: got %+v", sc)
	}
}

func TestProvider_ParseStreamLine_TextChunk(t *testing.T) {
	p := New()
	sc, err := p.ParseStreamLine(`data: {"type":"text","msg":"hello"}`)
	if err != nil {
		t.Fatalf("ParseStreamLine: %v", err)
	}
	if sc == nil {
		t.Fatal("expected chunk, got nil")
	}
	if sc.Type != "text" || sc.Text != "hello" {
		t.Errorf("text chunk: got %+v", sc)
	}
}

func TestProvider_ParseStreamLine_EmptyAndDone(t *testing.T) {
	p := New()
	for _, line := range []string{"", "data: [DONE]", "not-a-data-line"} {
		sc, err := p.ParseStreamLine(line)
		if err != nil {
			t.Errorf("line %q: unexpected err %v", line, err)
		}
		if sc != nil {
			t.Errorf("line %q: expected nil chunk, got %+v", line, sc)
		}
	}
}

func TestProvider_Send_RejectsWrongType(t *testing.T) {
	p := New()
	_, err := p.Send("not-a-yuanbao-request", "aid", "cid")
	if err == nil {
		t.Fatal("expected error when sending wrong type")
	}
}
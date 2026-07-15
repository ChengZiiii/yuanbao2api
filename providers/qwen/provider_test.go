package qwen

import (
	"errors"
	"testing"

	providers "yuanbao2api/providers"
)

func TestQwenProvider_Name(t *testing.T) {
	p := New()
	if p.Name() != "qwen" {
		t.Errorf("Name: got %q want qwen", p.Name())
	}
}

func TestQwenProvider_ModelsAdvertised(t *testing.T) {
	p := New()
	got := map[string]bool{}
	for _, m := range p.Models() {
		got[m.ID] = true
	}
	for _, want := range []string{"qwen-max", "qwen-plus"} {
		if !got[want] {
			t.Errorf("Models() missing %q", want)
		}
	}
}

func TestQwenProvider_NotImplemented(t *testing.T) {
	p := New()
	if _, _, err := p.BuildPrompt(nil, nil); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("BuildPrompt: want ErrNotImplemented, got %v", err)
	}
	if _, err := p.NewRequest("", providers.RequestOptions{}); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("NewRequest: want ErrNotImplemented, got %v", err)
	}
	if _, err := p.Send(nil, "aid", "cid"); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("Send: want ErrNotImplemented, got %v", err)
	}
}

func TestQwenProvider_ParseStreamLineAlwaysNil(t *testing.T) {
	p := New()
	for _, line := range []string{"", "data: anything", "data: [DONE]"} {
		sc, err := p.ParseStreamLine(line)
		if err != nil {
			t.Errorf("ParseStreamLine(%q): unexpected err %v", line, err)
		}
		if sc != nil {
			t.Errorf("ParseStreamLine(%q): expected nil chunk", line)
		}
	}
}
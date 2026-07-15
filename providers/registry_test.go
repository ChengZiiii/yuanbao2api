package provider

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestRegistry_RegisterAndLookup(t *testing.T) {
	r := NewRegistry()
	p := &stubProvider{name: "stub", models: []ModelInfo{{ID: "stub-1"}}}
	if err := r.Register(p); err != nil {
		t.Fatalf("Register: %v", err)
	}
	got, ok := r.Get("stub")
	if !ok || got != p {
		t.Fatalf("Get returned wrong provider: ok=%v got=%v", ok, got)
	}
}

func TestRegistry_RejectsDuplicateName(t *testing.T) {
	r := NewRegistry()
	p1 := &stubProvider{name: "x", models: []ModelInfo{{ID: "x-1"}}}
	p2 := &stubProvider{name: "x", models: []ModelInfo{{ID: "x-2"}}}
	if err := r.Register(p1); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	if err := r.Register(p2); err == nil {
		t.Fatalf("expected duplicate-name error, got nil")
	}
}

func TestRegistry_AllPreservesOrder(t *testing.T) {
	r := NewRegistry()
	r.Register(&stubProvider{name: "a", models: []ModelInfo{{ID: "a-1"}}})
	r.Register(&stubProvider{name: "b", models: []ModelInfo{{ID: "b-1"}}})
	r.Register(&stubProvider{name: "c", models: []ModelInfo{{ID: "c-1"}}})
	names := r.Names()
	if len(names) != 3 || names[0] != "a" || names[1] != "b" || names[2] != "c" {
		t.Errorf("Names ordering: got %v, want [a b c]", names)
	}
}

func TestRegistry_SetDefaultRequiresKnownProvider(t *testing.T) {
	r := NewRegistry()
	if err := r.SetDefault("nope"); err == nil {
		t.Errorf("SetDefault on unknown name should fail")
	}
	r.Register(&stubProvider{name: "yuanbao", models: []ModelInfo{{ID: "deep_seek_v3"}}})
	if err := r.SetDefault("yuanbao"); err != nil {
		t.Errorf("SetDefault on known name: %v", err)
	}
	if r.DefaultName() != "yuanbao" {
		t.Errorf("DefaultName: got %q want yuanbao", r.DefaultName())
	}
}

// TestRegistry_Route covers the four scenarios required by the spec:
// default-provider hit, cross-provider hit, unknown model, and
// placeholder (kimi) hit that should still return the provider.
func TestRegistry_Route(t *testing.T) {
	r := NewRegistry()
	yuanbao := &stubProvider{name: "yuanbao", models: []ModelInfo{{ID: "deep_seek_v3"}, {ID: "hunyuan"}}}
	qwen := &stubProvider{name: "qwen", models: []ModelInfo{{ID: "qwen-max"}}}
	kimi := &stubProvider{name: "kimi", models: []ModelInfo{{ID: "kimi-k2"}}}
	r.Register(yuanbao)
	r.Register(qwen)
	r.Register(kimi)
	if err := r.SetDefault("yuanbao"); err != nil {
		t.Fatalf("SetDefault: %v", err)
	}

	// 1) Default provider hit
	got, err := r.Route("deep_seek_v3")
	if err != nil || got != yuanbao {
		t.Errorf("default hit: got=%v err=%v want=yuanbao/nil", got, err)
	}

	// 2) Cross-provider hit
	got, err = r.Route("qwen-max")
	if err != nil || got != qwen {
		t.Errorf("cross-provider: got=%v err=%v want=qwen/nil", got, err)
	}

	// 3) Unknown model
	got, err = r.Route("nonexistent-model")
	if err == nil || got != nil {
		t.Errorf("unknown model: got=%v err=%v want=nil/non-nil", got, err)
	}
	if err != nil && !strings.Contains(err.Error(), "unknown model") {
		t.Errorf("unknown model: error %q should contain 'unknown model'", err.Error())
	}

	// 4) Placeholder (kimi) hit — Route does NOT check enablement; it
	// just returns the provider so callers see the right Name() when
	// they want to record metrics, etc. The "not implemented" check
	// lives in provider.Send / BuildPrompt.
	got, err = r.Route("kimi-k2")
	if err != nil || got != kimi {
		t.Errorf("placeholder hit: got=%v err=%v want=kimi/nil", got, err)
	}
}

func TestRegistry_RouteIsCaseInsensitive(t *testing.T) {
	r := NewRegistry()
	r.Register(&stubProvider{name: "yuanbao", models: []ModelInfo{{ID: "deep_seek_v3"}}})
	if err := r.SetDefault("yuanbao"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"Deep_Seek_V3", "DEEP_SEEK_V3", "deep_seek_v3"} {
		if _, err := r.Route(id); err != nil {
			t.Errorf("Route(%q) should hit, got err=%v", id, err)
		}
	}
}

// stubProvider is a minimal Provider implementation for unit tests.
type stubProvider struct {
	name   string
	models []ModelInfo
}

func (s *stubProvider) Name() string        { return s.name }
func (s *stubProvider) Models() []ModelInfo { return s.models }
func (s *stubProvider) BuildPrompt(_ []Message, _ []Tool) (string, string, error) {
	return "", "", errors.New("stub: BuildPrompt not implemented")
}
func (s *stubProvider) NewRequest(_ string, _ RequestOptions) (any, error) {
	return nil, errors.New("stub: NewRequest not implemented")
}
func (s *stubProvider) Send(_ any, _, _ string) (*http.Response, error) {
	return nil, errors.New("stub: Send not implemented")
}
func (s *stubProvider) ParseStreamLine(_ string) (*StreamChunk, error) {
	return nil, nil
}
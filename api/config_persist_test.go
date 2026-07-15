package api

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func intPointer(n int) *int {
	return &n
}

func stringPointer(s string) *string {
	return &s
}

func boolPointer(b bool) *bool {
	return &b
}

// cookiePointer builds a *YuanbaoCookie for tests that previously used
// *string. Centralised here so test cases do not have to repeat the
// struct literal every time.
func cookiePointer(hyToken, hyUser string) *YuanbaoCookie {
	return &YuanbaoCookie{HyToken: hyToken, HyUser: hyUser}
}

// newYuanbaoRuntimeConfig builds the standard Providers["yuanbao"]
// RuntimeConfig used by tests that exercise the new shape.
func newYuanbaoRuntimeConfig(cookie *YuanbaoCookie, maxC, qTimeout, cooldown *int) RuntimeConfig {
	return RuntimeConfig{
		Providers: map[string]ProviderConfig{
			"yuanbao": {
				Enabled:             boolPointer(true),
				Cookie:              cookie,
				MaxConcurrency:      maxC,
				QueueTimeoutSeconds: qTimeout,
				RequestCooldownMs:   cooldown,
			},
		},
		DefaultProvider: "yuanbao",
	}
}

func TestSaveAndLoadRuntimeConfig(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "runtime_config.json")
	t.Setenv("RUNTIME_CONFIG_PATH", path)

	// 保存
	maxC := 5
	qTimeout := 90
	cooldown := 250
	cfg := newYuanbaoRuntimeConfig(nil, &maxC, &qTimeout, &cooldown)
	if err := SaveRuntimeConfig(cfg); err != nil {
		t.Fatalf("SaveRuntimeConfig failed: %v", err)
	}

	// 加载
	loaded := LoadRuntimeConfig()
	yuanbao, ok := loaded.Providers["yuanbao"]
	if !ok {
		t.Fatalf("Providers[yuanbao] missing after load")
	}
	if yuanbao.MaxConcurrency == nil || *yuanbao.MaxConcurrency != 5 {
		t.Errorf("MaxConcurrency: got %v, want 5", yuanbao.MaxConcurrency)
	}
	if yuanbao.QueueTimeoutSeconds == nil || *yuanbao.QueueTimeoutSeconds != 90 {
		t.Errorf("QueueTimeoutSeconds: got %v, want 90", yuanbao.QueueTimeoutSeconds)
	}
	if yuanbao.RequestCooldownMs == nil || *yuanbao.RequestCooldownMs != 250 {
		t.Errorf("RequestCooldownMs: got %v, want 250", yuanbao.RequestCooldownMs)
	}

	// A later atomic save must replace the existing file on every supported OS.
	maxC2 := 6
	if err := SaveRuntimeConfig(newYuanbaoRuntimeConfig(nil, &maxC2, nil, nil)); err != nil {
		t.Fatalf("SaveRuntimeConfig replacement failed: %v", err)
	}
	replaced := LoadRuntimeConfig()
	if replaced.Providers["yuanbao"].MaxConcurrency == nil ||
		*replaced.Providers["yuanbao"].MaxConcurrency != 6 {
		t.Errorf("replacement MaxConcurrency: got %v, want 6", replaced.Providers["yuanbao"].MaxConcurrency)
	}
}

func TestLoadRuntimeConfig_MissingFile(t *testing.T) {
	t.Setenv("RUNTIME_CONFIG_PATH", filepath.Join(t.TempDir(), "nope.json"))

	loaded := LoadRuntimeConfig()
	if loaded.DefaultProvider != "" || loaded.Providers != nil {
		t.Errorf("expected empty config when file missing, got %+v", loaded)
	}
}

func TestLoadRuntimeConfig_CorruptFile(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "broken.json")
	if err := os.WriteFile(path, []byte("{not json"), 0600); err != nil {
		t.Fatalf("failed to write corrupt file: %v", err)
	}
	t.Setenv("RUNTIME_CONFIG_PATH", path)

	loaded := LoadRuntimeConfig()
	if loaded.Providers != nil {
		t.Errorf("expected empty config when file corrupt, got %+v", loaded)
	}
}

func TestLoadRuntimeConfig_TypeErrorDiscardsPartialDecode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "partial.json")
	data := []byte(`{"maxConcurrency":5,"queueTimeoutSeconds":"invalid","requestCooldownMs":250}`)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("failed to write invalid config: %v", err)
	}
	t.Setenv("RUNTIME_CONFIG_PATH", path)

	loaded := LoadRuntimeConfig()
	if loaded.Providers != nil {
		t.Errorf("expected type error to discard partially decoded values, got %+v", loaded)
	}
}

func TestSaveRuntimeConfig_WriteFailurePreservesTarget(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "runtime_config.json")
	t.Setenv("RUNTIME_CONFIG_PATH", path)

	original := []byte(`{"maxConcurrency":1}`)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatalf("failed to write original config: %v", err)
	}
	if err := os.Mkdir(path+".tmp", 0700); err != nil {
		t.Fatalf("failed to block temporary file path: %v", err)
	}

	maxC := 9
	if err := SaveRuntimeConfig(newYuanbaoRuntimeConfig(nil, &maxC, nil, nil)); err == nil {
		t.Fatal("SaveRuntimeConfig should report a temporary write failure")
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read original config: %v", err)
	}
	if string(got) != string(original) {
		t.Errorf("target changed after failed atomic save: got %q, want %q", got, original)
	}
}

func TestRuntimeConfigPath_DefaultVsOverride(t *testing.T) {
	// 默认路径
	os.Unsetenv("RUNTIME_CONFIG_PATH")
	if got := runtimeConfigPath(); got != "./runtime_config.json" {
		t.Errorf("default path: got %q, want ./runtime_config.json", got)
	}

	// 环境变量覆盖
	custom := "/tmp/custom_runtime.json"
	t.Setenv("RUNTIME_CONFIG_PATH", custom)
	if got := runtimeConfigPath(); got != custom {
		t.Errorf("custom path: got %q, want %q", got, custom)
	}
}

func TestSaveAndLoadRuntimeConfig_RoundTripYuanbaoCookie(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "runtime_config.json")
	t.Setenv("RUNTIME_CONFIG_PATH", path)

	cookie := cookiePointer("abc12345supersecret", "user-xyz")
	maxC := 5
	qTimeout := 90
	cooldown := 250
	cfg := newYuanbaoRuntimeConfig(cookie, &maxC, &qTimeout, &cooldown)
	if err := SaveRuntimeConfig(cfg); err != nil {
		t.Fatalf("SaveRuntimeConfig failed: %v", err)
	}

	loaded := LoadRuntimeConfig()
	yuanbao := loaded.Providers["yuanbao"]
	if yuanbao.Cookie == nil {
		t.Fatalf("Cookie: got nil, want %+v", cookie)
	}
	if yuanbao.Cookie.HyToken != "abc12345supersecret" || yuanbao.Cookie.HyUser != "user-xyz" {
		t.Errorf("Cookie: got %+v, want %+v", yuanbao.Cookie, cookie)
	}

	// The on-disk JSON must contain the canonical object form. Match
	// keys/values tolerantly because MarshalIndent inserts a space after
	// each colon.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if !bytes.Contains(raw, []byte(`"hyToken"`)) ||
		!bytes.Contains(raw, []byte(`"abc12345supersecret"`)) ||
		!bytes.Contains(raw, []byte(`"hyUser"`)) ||
		!bytes.Contains(raw, []byte(`"user-xyz"`)) {
		t.Errorf("persisted JSON missing expected fields: %s", raw)
	}
}

func TestLoadRuntimeConfig_LegacyYuanbaoCookieString(t *testing.T) {
	// Simulate a runtime_config.json written during the runtime-cookie
	// transition where yuanbaoCookie was stored as a flat string. The
	// new UnmarshalJSON translates it into Providers["yuanbao"].Cookie.
	path := filepath.Join(t.TempDir(), "runtime_config.json")
	legacy := []byte(`{"yuanbaoCookie":"hy_token=legacy; hy_user=old"}`)
	if err := os.WriteFile(path, legacy, 0600); err != nil {
		t.Fatalf("failed to write legacy config: %v", err)
	}
	t.Setenv("RUNTIME_CONFIG_PATH", path)

	loaded := LoadRuntimeConfig()
	yuanbao, ok := loaded.Providers["yuanbao"]
	if !ok {
		t.Fatalf("legacy string should populate Providers[yuanbao]")
	}
	if yuanbao.Cookie == nil {
		t.Fatalf("Cookie should have been parsed from legacy string, got nil")
	}
	if yuanbao.Cookie.HyToken != "legacy" {
		t.Errorf("HyToken: got %q, want %q", yuanbao.Cookie.HyToken, "legacy")
	}
	if yuanbao.Cookie.HyUser != "old" {
		t.Errorf("HyUser: got %q, want %q", yuanbao.Cookie.HyUser, "old")
	}
	if loaded.DefaultProvider != "yuanbao" {
		t.Errorf("DefaultProvider: got %q want yuanbao", loaded.DefaultProvider)
	}
}

func TestLoadRuntimeConfig_NewForm(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "runtime_config.json")
	t.Setenv("RUNTIME_CONFIG_PATH", path)

	original := cookiePointer("obj-token", "obj-user")
	if err := SaveRuntimeConfig(newYuanbaoRuntimeConfig(original, nil, nil, nil)); err != nil {
		t.Fatalf("SaveRuntimeConfig failed: %v", err)
	}

	loaded := LoadRuntimeConfig()
	yuanbao, ok := loaded.Providers["yuanbao"]
	if !ok || yuanbao.Cookie == nil {
		t.Fatalf("Cookie: got nil after object round-trip")
	}
	if yuanbao.Cookie.HyToken != "obj-token" || yuanbao.Cookie.HyUser != "obj-user" {
		t.Errorf("Cookie: got %+v, want %+v", yuanbao.Cookie, original)
	}
}

func TestLoadRuntimeConfig_LegacyFileWithoutYuanbaoCookie(t *testing.T) {
	// Simulate a runtime_config.json written before this feature existed.
	// The "yuanbaoCookie" key is absent; deserialization must still
	// yield Providers["yuanbao"] with the concurrency fields.
	path := filepath.Join(t.TempDir(), "runtime_config.json")
	legacy := []byte(`{"maxConcurrency":4,"queueTimeoutSeconds":60,"requestCooldownMs":100}`)
	if err := os.WriteFile(path, legacy, 0600); err != nil {
		t.Fatalf("failed to write legacy config: %v", err)
	}
	t.Setenv("RUNTIME_CONFIG_PATH", path)

	loaded := LoadRuntimeConfig()
	yuanbao, ok := loaded.Providers["yuanbao"]
	if !ok {
		t.Fatalf("legacy file should populate Providers[yuanbao]")
	}
	if yuanbao.Cookie != nil {
		t.Errorf("legacy file without yuanbaoCookie should yield nil cookie, got %+v", yuanbao.Cookie)
	}
	if yuanbao.MaxConcurrency == nil || *yuanbao.MaxConcurrency != 4 {
		t.Errorf("legacy file: MaxConcurrency not migrated: %+v", yuanbao.MaxConcurrency)
	}
}

func TestLoadRuntimeConfig_EmptyProviders(t *testing.T) {
	// File with neither providers nor legacy fields. UnmarshalJSON
	// recognises this as the empty new shape and produces Providers=nil.
	path := filepath.Join(t.TempDir(), "runtime_config.json")
	if err := os.WriteFile(path, []byte(`{}`), 0600); err != nil {
		t.Fatalf("failed to write empty config: %v", err)
	}
	t.Setenv("RUNTIME_CONFIG_PATH", path)

	loaded := LoadRuntimeConfig()
	if loaded.Providers != nil {
		t.Errorf("empty file should yield Providers=nil, got %+v", loaded.Providers)
	}
}

func TestAtomicRename_TargetDoesNotExist(t *testing.T) {
	dir := t.TempDir()
	tmp := filepath.Join(dir, "sub.tmp")
	target := filepath.Join(dir, "sub.json")

	want := []byte("hello-atomic")
	if err := os.WriteFile(tmp, want, 0600); err != nil {
		t.Fatalf("failed to seed tmp: %v", err)
	}

	if err := atomicRename(tmp, target); err != nil {
		t.Fatalf("atomicRename returned error when target absent: %v", err)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("target missing after rename: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("target content: got %q, want %q", got, want)
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Errorf("tmp should not exist after successful rename, stat err=%v", err)
	}
}

func TestAtomicRename_TargetExists(t *testing.T) {
	dir := t.TempDir()
	tmp := filepath.Join(dir, "sub.tmp")
	target := filepath.Join(dir, "sub.json")

	oldContent := []byte(`{"maxConcurrency":1}`)
	newContent := []byte(`{"maxConcurrency":7}`)

	if err := os.WriteFile(target, oldContent, 0600); err != nil {
		t.Fatalf("failed to seed target: %v", err)
	}
	if err := os.WriteFile(tmp, newContent, 0600); err != nil {
		t.Fatalf("failed to seed tmp: %v", err)
	}

	if err := atomicRename(tmp, target); err != nil {
		t.Fatalf("atomicRename returned error when target existed: %v", err)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("target missing after rename: %v", err)
	}
	if string(got) != string(newContent) {
		t.Errorf("target not replaced: got %q, want %q", got, newContent)
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Errorf("tmp should not exist after successful rename, stat err=%v", err)
	}
}

func TestAtomicRename_TmpMissing(t *testing.T) {
	dir := t.TempDir()
	tmp := filepath.Join(dir, "missing.tmp")
	target := filepath.Join(dir, "missing.json")

	// Pre-existing target whose contents MUST be preserved unchanged.
	original := []byte(`{"maxConcurrency":2}`)
	if err := os.WriteFile(target, original, 0600); err != nil {
		t.Fatalf("failed to seed target: %v", err)
	}

	if err := atomicRename(tmp, target); err == nil {
		t.Fatal("atomicRename should return non-nil error when tmp is absent")
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("target disappeared after failed rename: %v", err)
	}
	if string(got) != string(original) {
		t.Errorf("target changed after failed rename: got %q, want %q", got, original)
	}
}
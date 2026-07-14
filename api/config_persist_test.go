package api

import (
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

func TestSaveAndLoadRuntimeConfig(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "runtime_config.json")
	t.Setenv("RUNTIME_CONFIG_PATH", path)

	// 保存
	cfg := RuntimeConfig{
		MaxConcurrency:      intPointer(5),
		QueueTimeoutSeconds: intPointer(90),
		RequestCooldownMs:   intPointer(250),
	}
	if err := SaveRuntimeConfig(cfg); err != nil {
		t.Fatalf("SaveRuntimeConfig failed: %v", err)
	}

	// 加载
	loaded := LoadRuntimeConfig()
	if loaded.MaxConcurrency == nil || *loaded.MaxConcurrency != 5 {
		t.Errorf("MaxConcurrency: got %v, want 5", loaded.MaxConcurrency)
	}
	if loaded.QueueTimeoutSeconds == nil || *loaded.QueueTimeoutSeconds != 90 {
		t.Errorf("QueueTimeoutSeconds: got %v, want 90", loaded.QueueTimeoutSeconds)
	}
	if loaded.RequestCooldownMs == nil || *loaded.RequestCooldownMs != 250 {
		t.Errorf("RequestCooldownMs: got %v, want 250", loaded.RequestCooldownMs)
	}

	// A later atomic save must replace the existing file on every supported OS.
	if err := SaveRuntimeConfig(RuntimeConfig{MaxConcurrency: intPointer(6)}); err != nil {
		t.Fatalf("SaveRuntimeConfig replacement failed: %v", err)
	}
	replaced := LoadRuntimeConfig()
	if replaced.MaxConcurrency == nil || *replaced.MaxConcurrency != 6 {
		t.Errorf("replacement MaxConcurrency: got %v, want 6", replaced.MaxConcurrency)
	}
}

func TestLoadRuntimeConfig_MissingFile(t *testing.T) {
	t.Setenv("RUNTIME_CONFIG_PATH", filepath.Join(t.TempDir(), "nope.json"))

	loaded := LoadRuntimeConfig()
	if loaded.MaxConcurrency != nil || loaded.QueueTimeoutSeconds != nil || loaded.RequestCooldownMs != nil {
		t.Errorf("expected zero-valued config when file missing, got %+v", loaded)
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
	if loaded.MaxConcurrency != nil {
		t.Errorf("expected zero-valued config when file corrupt, got %+v", loaded)
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
	if loaded.MaxConcurrency != nil || loaded.QueueTimeoutSeconds != nil || loaded.RequestCooldownMs != nil {
		t.Errorf("expected type error to discard all partially decoded values, got %+v", loaded)
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

	if err := SaveRuntimeConfig(RuntimeConfig{MaxConcurrency: intPointer(9)}); err == nil {
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

	cookie := "abc12345supersecret"
	cfg := RuntimeConfig{
		MaxConcurrency:      intPointer(5),
		QueueTimeoutSeconds: intPointer(90),
		RequestCooldownMs:   intPointer(250),
		YuanbaoCookie:       stringPointer(cookie),
	}
	if err := SaveRuntimeConfig(cfg); err != nil {
		t.Fatalf("SaveRuntimeConfig failed: %v", err)
	}

	loaded := LoadRuntimeConfig()
	if loaded.YuanbaoCookie == nil {
		t.Fatalf("YuanbaoCookie: got nil, want %q", cookie)
	}
	if *loaded.YuanbaoCookie != cookie {
		t.Errorf("YuanbaoCookie: got %q, want %q", *loaded.YuanbaoCookie, cookie)
	}
}

func TestLoadRuntimeConfig_LegacyFileWithoutYuanbaoCookie(t *testing.T) {
	// Simulate a runtime_config.json written before this feature existed.
	// The "yuanbaoCookie" key is absent; deserialization must yield nil.
	path := filepath.Join(t.TempDir(), "runtime_config.json")
	legacy := []byte(`{"maxConcurrency":4,"queueTimeoutSeconds":60,"requestCooldownMs":100}`)
	if err := os.WriteFile(path, legacy, 0600); err != nil {
		t.Fatalf("failed to write legacy config: %v", err)
	}
	t.Setenv("RUNTIME_CONFIG_PATH", path)

	loaded := LoadRuntimeConfig()
	if loaded.YuanbaoCookie != nil {
		t.Errorf("legacy file should yield nil YuanbaoCookie, got %q", *loaded.YuanbaoCookie)
	}
	if loaded.MaxConcurrency == nil || *loaded.MaxConcurrency != 4 {
		t.Errorf("legacy file: MaxConcurrency not loaded correctly: %+v", loaded.MaxConcurrency)
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

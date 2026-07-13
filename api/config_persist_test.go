package api

import (
	"os"
	"path/filepath"
	"testing"
)

func intPointer(n int) *int {
	return &n
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

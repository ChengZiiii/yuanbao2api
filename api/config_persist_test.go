package api

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveAndLoadRuntimeConfig(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "runtime_config.json")
	t.Setenv("RUNTIME_CONFIG_PATH", path)

	// 保存
	cfg := RuntimeConfig{MaxConcurrency: 5, QueueTimeoutSeconds: 90, RequestCooldownMs: 250}
	if err := SaveRuntimeConfig(cfg); err != nil {
		t.Fatalf("SaveRuntimeConfig failed: %v", err)
	}

	// 加载
	loaded := LoadRuntimeConfig()
	if loaded.MaxConcurrency != 5 {
		t.Errorf("MaxConcurrency: got %d, want 5", loaded.MaxConcurrency)
	}
	if loaded.QueueTimeoutSeconds != 90 {
		t.Errorf("QueueTimeoutSeconds: got %d, want 90", loaded.QueueTimeoutSeconds)
	}
	if loaded.RequestCooldownMs != 250 {
		t.Errorf("RequestCooldownMs: got %d, want 250", loaded.RequestCooldownMs)
	}
}

func TestLoadRuntimeConfig_MissingFile(t *testing.T) {
	t.Setenv("RUNTIME_CONFIG_PATH", filepath.Join(t.TempDir(), "nope.json"))

	loaded := LoadRuntimeConfig()
	if loaded.MaxConcurrency != 0 || loaded.QueueTimeoutSeconds != 0 || loaded.RequestCooldownMs != 0 {
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
	if loaded.MaxConcurrency != 0 {
		t.Errorf("expected zero-valued config when file corrupt, got %+v", loaded)
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
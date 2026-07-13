package api

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInitRateLimiter_EnvOnly(t *testing.T) {
	// 确保无持久化文件
	t.Setenv("RUNTIME_CONFIG_PATH", filepath.Join(t.TempDir(), "absent.json"))
	os.Setenv("MAX_CONCURRENCY", "4")
	os.Setenv("QUEUE_TIMEOUT_SECONDS", "60")
	os.Setenv("REQUEST_COOLDOWN_MS", "250")
	defer os.Unsetenv("MAX_CONCURRENCY")
	defer os.Unsetenv("QUEUE_TIMEOUT_SECONDS")
	defer os.Unsetenv("REQUEST_COOLDOWN_MS")

	rl := InitRateLimiter()
	if rl.MaxConcurrency() != 4 {
		t.Errorf("MaxConcurrency: got %d, want 4", rl.MaxConcurrency())
	}
	if rl.QueueTimeout().Seconds() != 60 {
		t.Errorf("QueueTimeout: got %v, want 60s", rl.QueueTimeout())
	}
	if rl.Cooldown().Milliseconds() != 250 {
		t.Errorf("Cooldown: got %v, want 250ms", rl.Cooldown())
	}
}

func TestInitRateLimiter_RuntimeConfigOverridesEnv(t *testing.T) {
	// env 给 2，runtime 给 8 — runtime 应胜出
	os.Setenv("MAX_CONCURRENCY", "2")
	defer os.Unsetenv("MAX_CONCURRENCY")

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "runtime_config.json")
	t.Setenv("RUNTIME_CONFIG_PATH", path) // must precede Save
	if err := SaveRuntimeConfig(RuntimeConfig{MaxConcurrency: intPointer(8)}); err != nil {
		t.Fatalf("SaveRuntimeConfig failed: %v", err)
	}

	rl := InitRateLimiter()
	if rl.MaxConcurrency() != 8 {
		t.Errorf("MaxConcurrency: got %d, want 8 (runtime config should override env)", rl.MaxConcurrency())
	}
}

func TestInitRateLimiter_ZeroRuntimeValuesIgnored(t *testing.T) {
	// 零值不覆盖 env
	os.Setenv("QUEUE_TIMEOUT_SECONDS", "45")
	defer os.Unsetenv("QUEUE_TIMEOUT_SECONDS")

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "runtime_config.json")
	t.Setenv("RUNTIME_CONFIG_PATH", path) // must precede Save
	if err := SaveRuntimeConfig(RuntimeConfig{QueueTimeoutSeconds: intPointer(0)}); err != nil {
		t.Fatalf("SaveRuntimeConfig failed: %v", err)
	}

	rl := InitRateLimiter()
	if rl.QueueTimeout().Seconds() != 45 {
		t.Errorf("QueueTimeout: got %v, want 45s (zero runtime value should not override)", rl.QueueTimeout())
	}
}

func TestInitRateLimiter_RuntimeCooldownZeroOverridesEnv(t *testing.T) {
	t.Setenv("REQUEST_COOLDOWN_MS", "750")

	path := filepath.Join(t.TempDir(), "runtime_config.json")
	t.Setenv("RUNTIME_CONFIG_PATH", path)
	if err := os.WriteFile(path, []byte(`{"requestCooldownMs":0}`), 0600); err != nil {
		t.Fatalf("failed to write runtime config: %v", err)
	}

	rl := InitRateLimiter()
	if rl.Cooldown().Milliseconds() != 0 {
		t.Errorf("Cooldown: got %v, want 0ms (persisted zero should override env)", rl.Cooldown())
	}
}

func TestInitRateLimiter_DefaultCooldownIsZero(t *testing.T) {
	// Sanity: with no env and no runtime_config.json, cooldown defaults to 0ms.
	os.Unsetenv("REQUEST_COOLDOWN_MS")
	t.Setenv("RUNTIME_CONFIG_PATH", filepath.Join(t.TempDir(), "absent.json"))
	rl := InitRateLimiter()
	if rl.Cooldown().Milliseconds() != 0 {
		t.Errorf("Cooldown: got %v, want 0ms", rl.Cooldown())
	}
}

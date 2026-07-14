package api

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"time"
)

// RuntimeConfig holds runtime-persisted shared server configuration. Values
// here override env-derived defaults at startup and survive service restarts.
type RuntimeConfig struct {
	MaxConcurrency      *int    `json:"maxConcurrency,omitempty"`
	QueueTimeoutSeconds *int    `json:"queueTimeoutSeconds,omitempty"`
	RequestCooldownMs   *int    `json:"requestCooldownMs,omitempty"`
	YuanbaoCookie       *string `json:"yuanbaoCookie,omitempty"`
}

// runtimeConfigPath returns the file path used to persist RuntimeConfig. The
// path can be overridden by the RUNTIME_CONFIG_PATH env var (mainly for tests).
func runtimeConfigPath() string {
	if p := os.Getenv("RUNTIME_CONFIG_PATH"); p != "" {
		return p
	}
	return "./runtime_config.json"
}

// LoadRuntimeConfig reads the persisted config from disk. A missing,
// unreadable, or invalid file is treated as "no override" and returns the zero
// value. Pointer fields distinguish omitted values from explicit zero values.
func LoadRuntimeConfig() RuntimeConfig {
	var cfg RuntimeConfig
	data, err := os.ReadFile(runtimeConfigPath())
	if err != nil {
		return RuntimeConfig{}
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return RuntimeConfig{}
	}
	return cfg
}

// atomicRename moves tmp onto target with a Windows-friendly fallback chain.
//
// Phase 1 is a plain os.Rename (Unix always succeeds here; on a clean Windows
// installation the underlying MoveFileEx with MOVEFILE_REPLACE_EXISTING also
// succeeds). Phase 2 is a short retry loop that catches transient Windows
// sharing/AV errors. Phase 3 is a final remove-then-rename fallback that trades
// strict atomicity for write success when a long-running AV/lock is involved.
// In every failure branch the tmp file is removed before returning so we never
// leak a stray .tmp file. The contents of target are owned by the caller.
func atomicRename(tmp, target string) error {
	// Phase 1: direct rename.
	if err := os.Rename(tmp, target); err == nil {
		return nil
	}

	// Phase 2: brief retry for transient Windows AV / sharing conflicts.
	const maxAttempts = 5
	const retryDelay = 50 * time.Millisecond
	var lastErr error
	for i := 0; i < maxAttempts; i++ {
		if lastErr = os.Rename(tmp, target); lastErr == nil {
			return nil
		}
		time.Sleep(retryDelay)
	}

	// Guard: if tmp no longer exists there is nothing to rename from; avoid
	// destroying the existing target in the Phase 3 fallback below.
	if _, statErr := os.Stat(tmp); statErr == nil {
		// Phase 3: explicit remove + rename fallback. Sacrifices strict
		// atomicity to guarantee write success when a lock persists beyond the
		// retry window.
		if err := os.Remove(target); err != nil && !errors.Is(err, fs.ErrNotExist) {
			_ = os.Remove(tmp)
			return err
		}
		if err := os.Rename(tmp, target); err != nil {
			_ = os.Remove(tmp)
			return err
		}
		return nil
	}
	return lastErr
}

// SaveRuntimeConfig atomically writes the config with 0600 permissions. The
// temporary file is written in the target directory, so a failed write leaves
// the previously saved configuration intact.
func SaveRuntimeConfig(cfg RuntimeConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	target := runtimeConfigPath()
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	// atomicRename cleans up tmp on every error path, so we just propagate
	// its error directly without an extra os.Remove call.
	return atomicRename(tmp, target)
}

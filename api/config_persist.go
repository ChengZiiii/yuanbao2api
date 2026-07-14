package api

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"strings"
	"time"
)

// YuanbaoCookie holds the two halves of a Yuanbao session cookie.
// HeaderValue assembles them in the "hy_token=...; hy_user=..." order
// expected by the upstream Yuanbao service.
type YuanbaoCookie struct {
	HyToken string `json:"hyToken"`
	HyUser  string `json:"hyUser"`
}

// UnmarshalJSON accepts both the canonical object form and the legacy
// string form ("hy_token=xxx; hy_user=yyy") produced by an earlier
// runtime-cookie version of the panel. The legacy form is only used to
// migrate existing runtime_config.json files; new writes always go
// through the object form.
func (c *YuanbaoCookie) UnmarshalJSON(data []byte) error {
	// 1) Try the canonical object form first.
	type alias YuanbaoCookie
	var s alias
	if err := json.Unmarshal(data, &s); err == nil {
		*c = YuanbaoCookie(s)
		return nil
	}
	// 2) Fall back to the legacy string form.
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	return c.parseLegacyString(str)
}

// parseLegacyString parses "hy_token=xxx; hy_user=yyy" into the
// struct fields. Unknown keys are ignored; pairs missing '=' are
// skipped. The parser is intentionally lenient so that a stray space
// or a trailing semicolon does not break loading.
func (c *YuanbaoCookie) parseLegacyString(s string) error {
	for _, pair := range strings.Split(s, ";") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		idx := strings.Index(pair, "=")
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(pair[:idx])
		val := strings.TrimSpace(pair[idx+1:])
		switch key {
		case "hy_token":
			c.HyToken = val
		case "hy_user":
			c.HyUser = val
		}
	}
	return nil
}

// HeaderValue assembles the Cookie header for upstream requests.
// Empty fields are omitted; if both fields are empty the result is "".
// A nil receiver is treated as "empty" and returns "".
func (c *YuanbaoCookie) HeaderValue() string {
	if c == nil {
		return ""
	}
	var parts []string
	if c.HyToken != "" {
		parts = append(parts, "hy_token="+c.HyToken)
	}
	if c.HyUser != "" {
		parts = append(parts, "hy_user="+c.HyUser)
	}
	return strings.Join(parts, "; ")
}

// RuntimeConfig holds runtime-persisted shared server configuration. Values
// here override env-derived defaults at startup and survive service restarts.
type RuntimeConfig struct {
	MaxConcurrency      *int           `json:"maxConcurrency,omitempty"`
	QueueTimeoutSeconds *int           `json:"queueTimeoutSeconds,omitempty"`
	RequestCooldownMs   *int           `json:"requestCooldownMs,omitempty"`
	YuanbaoCookie       *YuanbaoCookie `json:"yuanbaoCookie,omitempty"`
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
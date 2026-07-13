package api

import (
	"encoding/json"
	"os"
)

// RuntimeConfig holds runtime-persisted shared server configuration. Values
// here override env-derived defaults at startup and survive service restarts.
type RuntimeConfig struct {
	MaxConcurrency      *int `json:"maxConcurrency,omitempty"`
	QueueTimeoutSeconds *int `json:"queueTimeoutSeconds,omitempty"`
	RequestCooldownMs   *int `json:"requestCooldownMs,omitempty"`
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
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

package api

import (
	"encoding/json"
	"os"
)

// RuntimeConfig holds runtime-persisted shared server configuration. Values
// here override env-derived defaults at startup and survive service restarts.
type RuntimeConfig struct {
	MaxConcurrency      int `json:"maxConcurrency,omitempty"`
	QueueTimeoutSeconds int `json:"queueTimeoutSeconds,omitempty"`
	RequestCooldownMs   int `json:"requestCooldownMs,omitempty"`
}

// runtimeConfigPath returns the file path used to persist RuntimeConfig. The
// path can be overridden by the RUNTIME_CONFIG_PATH env var (mainly for tests).
func runtimeConfigPath() string {
	if p := os.Getenv("RUNTIME_CONFIG_PATH"); p != "" {
		return p
	}
	return "./runtime_config.json"
}

// LoadRuntimeConfig reads the persisted config from disk. A missing or
// unparseable file is treated as "no override" and returns the zero value;
// callers must check for non-zero fields before overriding env defaults.
func LoadRuntimeConfig() RuntimeConfig {
	var cfg RuntimeConfig
	data, err := os.ReadFile(runtimeConfigPath())
	if err != nil {
		return cfg // missing file or unreadable -> zero value
	}
	_ = json.Unmarshal(data, &cfg) // corrupt JSON -> zero value
	return cfg
}

// SaveRuntimeConfig writes the config to disk with 0600 permissions. Returns
// an error if the write fails; callers should log and continue (do not abort
// the request just because persistence failed).
func SaveRuntimeConfig(cfg RuntimeConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(runtimeConfigPath(), data, 0600)
}
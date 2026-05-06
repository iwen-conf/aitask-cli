package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// GlobalConfig is the persistent CLI configuration stored at ~/.aitask/config.json.
// It holds settings shared across invocations (e.g. backend URL) and is independent
// of per-project state under <repo>/.aitask/.
type GlobalConfig struct {
	ServerURL string `json:"server_url,omitempty"`
}

const globalConfigFileName = "config.json"

func globalConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, AITaskDirName, globalConfigFileName), nil
}

// LoadGlobalConfig reads ~/.aitask/config.json. Missing file returns a zero-value
// config without error so callers can treat it as "no overrides set".
func LoadGlobalConfig() (GlobalConfig, error) {
	path, err := globalConfigPath()
	if err != nil {
		return GlobalConfig{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return GlobalConfig{}, nil
		}
		return GlobalConfig{}, err
	}
	var cfg GlobalConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return GlobalConfig{}, err
	}
	cfg.ServerURL = strings.TrimSpace(cfg.ServerURL)
	return cfg, nil
}

// SaveGlobalConfig writes ~/.aitask/config.json atomically (write to .tmp + rename)
// with mode 0600 so the file isn't world-readable.
func SaveGlobalConfig(cfg GlobalConfig) error {
	path, err := globalConfigPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// resolveDefaultServerURL implements the lookup chain:
// AITASK_SERVER_URL env > ~/.aitask/config.json > built-in defaultServerURL.
// The --server flag is layered on top by cobra and overrides this fallback.
func resolveDefaultServerURL() string {
	if v := strings.TrimSpace(os.Getenv("AITASK_SERVER_URL")); v != "" {
		return v
	}
	if cfg, err := LoadGlobalConfig(); err == nil && cfg.ServerURL != "" {
		return cfg.ServerURL
	}
	return defaultServerURL
}

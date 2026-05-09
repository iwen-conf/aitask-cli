// Package conf loads OpenViking CLI connection configuration.
//
// It honors the upstream `openviking` CLI convention so users that already
// have `~/.openviking/ovcli.conf` (or set `OPENVIKING_CLI_CONFIG_FILE`) can
// reuse it without re-typing endpoint and API key in the AITask settings UI.
package conf

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// EnvConfigPath is the environment variable that overrides the default
// configuration file location, mirroring the upstream OpenViking CLI.
const EnvConfigPath = "OPENVIKING_CLI_CONFIG_FILE"

// DefaultRelativePath is the conventional config file location under $HOME.
const DefaultRelativePath = ".openviking/ovcli.conf"

// ErrNotFound is returned when neither the env-overridden nor the default
// config file exists. Callers can treat this as "no ovcli.conf available".
var ErrNotFound = errors.New("openviking ovcli.conf not found")

// Config models the JSON schema documented by OpenViking's CLI quickstart.
//
// Only `URL` and `APIKey` are required by upstream; the remaining fields are
// accepted as optional extensions so newer OpenViking releases that ship more
// metadata (workspace, account, root key) can be loaded transparently.
type Config struct {
	URL         string `json:"url"`
	APIKey      string `json:"api_key"`
	RootAPIKey  string `json:"root_api_key,omitempty"`
	Account     string `json:"account,omitempty"`
	Workspace   string `json:"workspace,omitempty"`
	WorkspaceID string `json:"workspace_id,omitempty"`
	Namespace   string `json:"namespace,omitempty"`
	ProjectID   string `json:"project_id,omitempty"`

	// Source is the absolute path the config was loaded from. Not serialized.
	Source string `json:"-"`
}

// HasCredentials reports whether the loaded config has the minimum needed
// to talk to an OpenViking server (URL + an API key of either flavour).
func (c Config) HasCredentials() bool {
	return strings.TrimSpace(c.URL) != "" && (strings.TrimSpace(c.APIKey) != "" || strings.TrimSpace(c.RootAPIKey) != "")
}

// EffectiveWorkspace returns whichever workspace identifier is set, preferring
// the explicit ID variant.
func (c Config) EffectiveWorkspace() string {
	if v := strings.TrimSpace(c.WorkspaceID); v != "" {
		return v
	}
	return strings.TrimSpace(c.Workspace)
}

// MaskedAPIKey returns the API key with all but the last four characters
// replaced by `*`, suitable for logging or `config show`.
func MaskedAPIKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	if len(key) <= 4 {
		return strings.Repeat("*", len(key))
	}
	return strings.Repeat("*", len(key)-4) + key[len(key)-4:]
}

// DefaultPath resolves the config file path that Load would read.
//
// Precedence: $OPENVIKING_CLI_CONFIG_FILE > $HOME/.openviking/ovcli.conf.
// Returns an error only when $HOME cannot be resolved AND no env override is set.
func DefaultPath() (string, error) {
	if override := strings.TrimSpace(os.Getenv(EnvConfigPath)); override != "" {
		return override, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, DefaultRelativePath), nil
}

// Load reads the configuration from the default path.
func Load() (Config, error) {
	path, err := DefaultPath()
	if err != nil {
		return Config{}, err
	}
	return LoadFromPath(path)
}

// LoadFromPath reads and parses the configuration from the given path.
//
// A non-existent file maps to ErrNotFound so callers can distinguish
// "missing" from "malformed".
func LoadFromPath(path string) (Config, error) {
	if strings.TrimSpace(path) == "" {
		return Config{}, ErrNotFound
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, fmt.Errorf("%w: %s", ErrNotFound, path)
		}
		return Config{}, fmt.Errorf("read %s: %w", path, err)
	}
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", path, err)
	}
	cfg.URL = strings.TrimSpace(cfg.URL)
	cfg.APIKey = strings.TrimSpace(cfg.APIKey)
	cfg.RootAPIKey = strings.TrimSpace(cfg.RootAPIKey)
	cfg.Account = strings.TrimSpace(cfg.Account)
	cfg.Workspace = strings.TrimSpace(cfg.Workspace)
	cfg.WorkspaceID = strings.TrimSpace(cfg.WorkspaceID)
	cfg.Namespace = strings.TrimSpace(cfg.Namespace)
	cfg.ProjectID = strings.TrimSpace(cfg.ProjectID)
	cfg.Source = path
	return cfg, nil
}

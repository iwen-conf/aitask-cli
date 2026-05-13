package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// GlobalConfig is the persistent CLI configuration stored at ~/.aitask/config.json.
// It holds settings shared across invocations (backend URL, multi-identity profiles,
// OpenViking server credentials) and is independent of per-project state under
// <repo>/.aitask/.
type GlobalConfig struct {
	ServerURL     string                   `json:"server_url,omitempty"`
	ActiveProfile string                   `json:"active_profile,omitempty"`
	Profiles      map[string]ProfileRecord `json:"profiles,omitempty"`
	OpenViking    *OpenVikingGlobal        `json:"openviking,omitempty"`
}

// OpenVikingGlobal stores the OpenViking server endpoint and (optional) API key
// at the user level. Per-project Namespace and WorkspaceID live in
// .aitask/project.md instead.
type OpenVikingGlobal struct {
	ServerURL string `json:"server_url,omitempty"`
	APIKey    string `json:"api_key,omitempty"`
}

// ProfileRecord caches identity hints for a stored profile so `auth profile list`
// can show useful context without hitting the backend on every invocation.
// The token itself is never stored here (see token_store.go redline).
type ProfileRecord struct {
	AgentID   string `json:"agent_id,omitempty"`
	AgentType string `json:"agent_type,omitempty"`
	Role      string `json:"role,omitempty"`
	ServerURL string `json:"server_url,omitempty"`
	StoredAt  string `json:"stored_at,omitempty"`
}

const (
	globalConfigFileName = "config.json"
	DefaultProfileName   = "default"
	envProfileName       = "AITASK_PROFILE"
	maxProfileNameLen    = 32
)

var profileNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

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
	cfg.ActiveProfile = strings.TrimSpace(cfg.ActiveProfile)
	if cfg.Profiles == nil {
		cfg.Profiles = map[string]ProfileRecord{}
	}
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
	if cfg.Profiles == nil {
		cfg.Profiles = map[string]ProfileRecord{}
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

// resolveDefaultProfile picks the profile name to use when --profile is not passed.
// Priority: AITASK_PROFILE env > active_profile in config.json > DefaultProfileName.
// The --profile flag is layered on top by cobra (PersistentPreRunE) and overrides this.
func resolveDefaultProfile() string {
	if v := normalizeProfileName(os.Getenv(envProfileName)); v != "" {
		return v
	}
	if cfg, err := LoadGlobalConfig(); err == nil {
		if v := normalizeProfileName(cfg.ActiveProfile); v != "" {
			return v
		}
	}
	return DefaultProfileName
}

// resolveEffectiveProfile is the PersistentPreRunE entry point: an explicit
// --profile flag (when non-empty) wins; otherwise we fall through to env / config.
// An invalid explicit value is rejected loudly so the user sees the typo
// instead of silently running as "default".
func resolveEffectiveProfile(flagValue string) (string, error) {
	if strings.TrimSpace(flagValue) != "" {
		v, err := ValidateProfileName(flagValue)
		if err != nil {
			return "", fmt.Errorf("--profile: %w", err)
		}
		return v, nil
	}
	return resolveDefaultProfile(), nil
}

// normalizeProfileName lowercases and trims a profile name; returns "" if invalid
// (so callers can fall through to defaults instead of erroring).
func normalizeProfileName(raw string) string {
	v := strings.ToLower(strings.TrimSpace(raw))
	if v == "" || !profileNamePattern.MatchString(v) || len(v) > maxProfileNameLen {
		return ""
	}
	return v
}

// ValidateProfileName returns the canonical (lowercased) profile name or an error
// describing why the name is unacceptable. Used by `auth profile add/use/remove`
// to give users actionable feedback rather than silently coercing to default.
func ValidateProfileName(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errors.New("profile name cannot be empty")
	}
	v := strings.ToLower(trimmed)
	if len(v) > maxProfileNameLen {
		return "", fmt.Errorf("profile name too long (max %d chars)", maxProfileNameLen)
	}
	if !profileNamePattern.MatchString(v) {
		return "", errors.New("profile name must match [a-z0-9][a-z0-9_-]* (lowercase letters, digits, underscore, dash)")
	}
	return v, nil
}

// SortedProfileNames returns the keys of cfg.Profiles in deterministic order so
// `auth profile list` output is stable across invocations.
func (c GlobalConfig) SortedProfileNames() []string {
	names := make([]string, 0, len(c.Profiles))
	for name := range c.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

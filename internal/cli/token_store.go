package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const keychainServiceName = "aitask-cli"
const tokenStoreFileModeEnv = "AITASK_TOKEN_STORE"

type TokenStore struct {
	homeDir string
}

func NewTokenStore() (*TokenStore, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return &TokenStore{homeDir: home}, nil
}

// Save persists a token for (serverURL, profile). Empty profile is normalized to
// DefaultProfileName so callers can pass through without thinking about it.
// On darwin we prefer Keychain; on save failure or when AITASK_TOKEN_STORE=file
// is set, we fall back to ~/.aitask/credentials/<account>.token.
func (s *TokenStore) Save(serverURL string, profile string, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return errors.New("token cannot be empty")
	}
	account := tokenAccount(serverURL, profile)
	if runtime.GOOS == "darwin" && !forceFileTokenStore() {
		if err := s.saveToKeychain(account, token); err == nil {
			// Best-effort sweep of legacy single-token entries when storing the
			// default profile so users coming from pre-profile installs end up
			// with a single canonical entry.
			if normalizeProfileName(profile) == DefaultProfileName || profile == "" {
				_ = s.deleteFromKeychain(legacyTokenAccount(serverURL))
				_ = os.Remove(s.legacyFilePath(legacyTokenAccount(serverURL)))
			}
			return nil
		}
	}
	if err := s.saveToFile(account, token); err != nil {
		return err
	}
	if normalizeProfileName(profile) == DefaultProfileName || profile == "" {
		_ = os.Remove(s.legacyFilePath(legacyTokenAccount(serverURL)))
	}
	return nil
}

// Load returns the stored token for (serverURL, profile). When the new
// profile-aware slot is empty AND the requested profile is the default, we
// transparently adopt any legacy single-token entry (pre-profile installs)
// and migrate it to the new slot so subsequent reads see it natively.
func (s *TokenStore) Load(serverURL string, profile string) (string, error) {
	account := tokenAccount(serverURL, profile)
	if token, err := s.loadAt(account); err == nil {
		return token, nil
	}
	// Legacy migration only applies for the default profile so we don't
	// silently steal a credential into an unrelated named profile.
	if normalizeProfileName(profile) == DefaultProfileName || profile == "" {
		legacy := legacyTokenAccount(serverURL)
		if token, err := s.loadAt(legacy); err == nil {
			_ = s.Save(serverURL, DefaultProfileName, token)
			_ = stubProfileRegistry(serverURL, DefaultProfileName)
			return token, nil
		}
	}
	return "", fmt.Errorf("agent token not found for profile %q, run `aitask auth token import --profile %s` or `aitask auth bind --code ... --profile %s`",
		effectiveProfile(profile), effectiveProfile(profile), effectiveProfile(profile))
}

// stubProfileRegistry inserts a placeholder ProfileRecord so `auth profile list`
// surfaces silently-migrated entries. Identity hints stay blank until the user
// re-runs `auth bind/import` (which calls upsertProfileRecord with whoami data).
func stubProfileRegistry(serverURL, profile string) error {
	cfg, err := LoadGlobalConfig()
	if err != nil {
		return err
	}
	if cfg.Profiles == nil {
		cfg.Profiles = map[string]ProfileRecord{}
	}
	if _, exists := cfg.Profiles[profile]; exists {
		return nil
	}
	cfg.Profiles[profile] = ProfileRecord{ServerURL: serverURL}
	return SaveGlobalConfig(cfg)
}

// Delete removes the token slot for (serverURL, profile). Both backends are
// attempted so the call is idempotent regardless of where the entry lives.
// Returns nil even when nothing was found — this is a "best-effort wipe".
func (s *TokenStore) Delete(serverURL string, profile string) error {
	account := tokenAccount(serverURL, profile)
	if runtime.GOOS == "darwin" && !forceFileTokenStore() {
		_ = s.deleteFromKeychain(account)
	}
	_ = os.Remove(s.filePath(account))
	return nil
}

// loadAt is the backend-fan-out half of Load — try keychain first on darwin,
// fall through to the file backend on miss or when AITASK_TOKEN_STORE=file.
func (s *TokenStore) loadAt(account string) (string, error) {
	if runtime.GOOS == "darwin" && !forceFileTokenStore() {
		if token, err := s.loadFromKeychain(account); err == nil {
			return token, nil
		}
	}
	return s.loadFromFile(account)
}

func forceFileTokenStore() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv(tokenStoreFileModeEnv)), "file")
}

// filePath is the new profile-aware credentials path.
// Layout: ~/.aitask/credentials/server-<8hex>__<profile>.token
func (s *TokenStore) filePath(account string) string {
	dir := filepath.Join(s.homeDir, ".aitask", "credentials")
	return filepath.Join(dir, account+".token")
}

// legacyFilePath mirrors the pre-profile path so migration can wipe it after
// a successful adopt-and-rewrite.
func (s *TokenStore) legacyFilePath(legacyAccount string) string {
	dir := filepath.Join(s.homeDir, ".aitask", "credentials")
	return filepath.Join(dir, legacyAccount+".token")
}

func (s *TokenStore) saveToFile(account string, token string) error {
	path := s.filePath(account)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(token+"\n"), 0o600)
}

func (s *TokenStore) loadFromFile(account string) (string, error) {
	path := s.filePath(account)
	payload, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(payload)), nil
}

func (s *TokenStore) saveToKeychain(account string, token string) error {
	cmd := exec.Command("security", "add-generic-password", "-a", account, "-s", keychainServiceName, "-w", token, "-U")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("keychain save failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (s *TokenStore) loadFromKeychain(account string) (string, error) {
	cmd := exec.Command("security", "find-generic-password", "-a", account, "-s", keychainServiceName, "-w")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func (s *TokenStore) deleteFromKeychain(account string) error {
	cmd := exec.Command("security", "delete-generic-password", "-a", account, "-s", keychainServiceName)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("keychain delete failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// tokenAccount derives the storage key for (serverURL, profile). The serverURL
// is hashed (8 byte prefix is plenty for collision resistance among a handful
// of backends) and the profile is appended in cleartext so humans inspecting
// keychain or ~/.aitask/credentials/ can tell which slot is which.
func tokenAccount(serverURL string, profile string) string {
	return serverHashAccount(serverURL) + "__" + effectiveProfile(profile)
}

// legacyTokenAccount is the pre-profile flat key, kept around purely so the
// one-time migration in Load can find old installs.
func legacyTokenAccount(serverURL string) string {
	return serverHashAccount(serverURL)
}

func serverHashAccount(serverURL string) string {
	value := strings.TrimSpace(strings.ToLower(serverURL))
	if value == "" {
		value = "http://127.0.0.1:8080"
	}
	hash := sha256.Sum256([]byte(value))
	return "server-" + hex.EncodeToString(hash[:8])
}

func effectiveProfile(profile string) string {
	if v := normalizeProfileName(profile); v != "" {
		return v
	}
	return DefaultProfileName
}

package cli

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// withFakeHome reroutes user.HomeDir() to a temp dir for the duration of t and
// forces file-mode storage so the test never touches the real Keychain.
func withFakeHome(t *testing.T) (*TokenStore, string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv(tokenStoreFileModeEnv, "file")
	store, err := NewTokenStore()
	if err != nil {
		t.Fatalf("NewTokenStore: %v", err)
	}
	return store, dir
}

func TestTokenAccount_PerProfile(t *testing.T) {
	a := tokenAccount("http://localhost:8080", "claude")
	b := tokenAccount("http://localhost:8080", "codex")
	if a == b {
		t.Fatalf("accounts must differ across profiles, got %s twice", a)
	}
	if !strings.HasSuffix(a, "__claude") {
		t.Fatalf("expected suffix __claude, got %s", a)
	}
	if !strings.HasSuffix(b, "__codex") {
		t.Fatalf("expected suffix __codex, got %s", b)
	}
}

func TestTokenAccount_DefaultProfileNormalization(t *testing.T) {
	cases := []string{"", "default", "DEFAULT", "  default  "}
	expected := tokenAccount("http://localhost:8080", "default")
	for _, c := range cases {
		if got := tokenAccount("http://localhost:8080", c); got != expected {
			t.Fatalf("profile %q: expected %s, got %s", c, expected, got)
		}
	}
}

func TestTokenStore_ProfileIsolation(t *testing.T) {
	store, _ := withFakeHome(t)
	const server = "http://example.test:1234"

	if err := store.Save(server, "claude", "tok-claude"); err != nil {
		t.Fatalf("save claude: %v", err)
	}
	if err := store.Save(server, "codex", "tok-codex"); err != nil {
		t.Fatalf("save codex: %v", err)
	}

	gotClaude, err := store.Load(server, "claude")
	if err != nil {
		t.Fatalf("load claude: %v", err)
	}
	if gotClaude != "tok-claude" {
		t.Fatalf("claude token mismatch: got %q", gotClaude)
	}

	gotCodex, err := store.Load(server, "codex")
	if err != nil {
		t.Fatalf("load codex: %v", err)
	}
	if gotCodex != "tok-codex" {
		t.Fatalf("codex token mismatch: got %q", gotCodex)
	}
}

func TestTokenStore_LoadMissingProfile(t *testing.T) {
	store, _ := withFakeHome(t)
	_, err := store.Load("http://example.test:1234", "gemini")
	if err == nil {
		t.Fatalf("expected error when profile is missing, got nil")
	}
	if !strings.Contains(err.Error(), "gemini") {
		t.Fatalf("error message should mention the missing profile name; got %v", err)
	}
}

func TestTokenStore_LegacyMigration_DefaultProfileOnly(t *testing.T) {
	if runtime.GOOS != "darwin" {
		// File-mode is the same on every OS; force it via env above so this
		// test exercises the same code path on linux CI.
	}
	store, home := withFakeHome(t)
	const server = "http://legacy.test:9999"

	// Plant a legacy single-token credential file (pre-profile layout).
	legacyAccount := legacyTokenAccount(server)
	legacyDir := filepath.Join(home, ".aitask", "credentials")
	if err := os.MkdirAll(legacyDir, 0o700); err != nil {
		t.Fatalf("mkdir legacy: %v", err)
	}
	legacyPath := filepath.Join(legacyDir, legacyAccount+".token")
	if err := os.WriteFile(legacyPath, []byte("legacy-token\n"), 0o600); err != nil {
		t.Fatalf("write legacy: %v", err)
	}

	// Loading the default profile should adopt the legacy token transparently.
	got, err := store.Load(server, DefaultProfileName)
	if err != nil {
		t.Fatalf("load default: %v", err)
	}
	if got != "legacy-token" {
		t.Fatalf("expected legacy-token to be adopted, got %q", got)
	}

	// And the migration should have wiped the legacy file so subsequent
	// operations don't double-bookkeep the same credential.
	if _, err := os.Stat(legacyPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy file should have been removed after adoption, stat err=%v", err)
	}

	// Loading a non-default profile must NOT pick up legacy data.
	_, err = store.Load(server, "claude")
	if err == nil {
		t.Fatalf("legacy token should not be visible to a non-default profile")
	}
}

func TestTokenStore_Delete(t *testing.T) {
	store, _ := withFakeHome(t)
	const server = "http://example.test:1234"
	if err := store.Save(server, "ephemeral", "tok"); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := store.Delete(server, "ephemeral"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := store.Load(server, "ephemeral"); err == nil {
		t.Fatalf("token should have been deleted")
	}
	// Idempotent — second delete is fine.
	if err := store.Delete(server, "ephemeral"); err != nil {
		t.Fatalf("second delete should be a no-op, got %v", err)
	}
}

func TestValidateProfileName(t *testing.T) {
	good := []string{"default", "claude", "codex", "gemini", "user-1", "team_a", "x"}
	for _, name := range good {
		if v, err := ValidateProfileName(name); err != nil || v != strings.ToLower(name) {
			t.Fatalf("expected %q to validate, got v=%q err=%v", name, v, err)
		}
	}
	bad := []string{"", "  ", "Has Space", "weird/slash", "-leading", "_underscore-leading", strings.Repeat("a", 33)}
	for _, name := range bad {
		if _, err := ValidateProfileName(name); err == nil {
			t.Fatalf("expected %q to be rejected", name)
		}
	}
}

func TestResolveDefaultProfile_Precedence(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv(envProfileName, "")

	// 1. Nothing set → default.
	if got := resolveDefaultProfile(); got != DefaultProfileName {
		t.Fatalf("expected default, got %q", got)
	}

	// 2. config.active_profile is honoured.
	cfg := GlobalConfig{ActiveProfile: "claude"}
	if err := SaveGlobalConfig(cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	if got := resolveDefaultProfile(); got != "claude" {
		t.Fatalf("expected claude from config, got %q", got)
	}

	// 3. Env beats config.
	t.Setenv(envProfileName, "gemini")
	if got := resolveDefaultProfile(); got != "gemini" {
		t.Fatalf("expected gemini from env, got %q", got)
	}

	// 4. Invalid env falls through to config.
	t.Setenv(envProfileName, "BAD/NAME")
	if got := resolveDefaultProfile(); got != "claude" {
		t.Fatalf("invalid env should fall through to config, got %q", got)
	}
}

func TestResolveEffectiveProfile_FlagOverrides(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv(envProfileName, "gemini")
	if err := SaveGlobalConfig(GlobalConfig{ActiveProfile: "claude"}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	got, err := resolveEffectiveProfile("codex")
	if err != nil {
		t.Fatalf("expected flag to win, err=%v", err)
	}
	if got != "codex" {
		t.Fatalf("expected codex from flag, got %q", got)
	}

	// Empty flag → fall back through env > config > default.
	got, err = resolveEffectiveProfile("")
	if err != nil {
		t.Fatalf("empty flag should fall through, err=%v", err)
	}
	if got != "gemini" {
		t.Fatalf("expected gemini from env, got %q", got)
	}

	// Invalid flag must error loudly so typos surface.
	if _, err := resolveEffectiveProfile("Bad Name"); err == nil {
		t.Fatalf("invalid flag should error")
	}
}

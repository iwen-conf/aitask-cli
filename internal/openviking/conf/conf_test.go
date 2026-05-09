package conf

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultPathHonoursEnvOverride(t *testing.T) {
	t.Setenv(EnvConfigPath, "/tmp/custom-ovcli.conf")
	got, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath() error = %v", err)
	}
	if got != "/tmp/custom-ovcli.conf" {
		t.Fatalf("expected env override path, got %q", got)
	}
}

func TestDefaultPathFallsBackToHome(t *testing.T) {
	t.Setenv(EnvConfigPath, "")
	t.Setenv("HOME", "/tmp/fakehome")
	got, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath() error = %v", err)
	}
	want := filepath.Join("/tmp/fakehome", DefaultRelativePath)
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestLoadFromPathMissingFileReturnsNotFound(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.conf")
	_, err := LoadFromPath(missing)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestLoadFromPathParsesValidConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ovcli.conf")
	body := `{
  "url": "  http://localhost:1933  ",
  "api_key": "secret-1234",
  "workspace_id": "ws_42",
  "namespace": "team-a"
}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	cfg, err := LoadFromPath(path)
	if err != nil {
		t.Fatalf("LoadFromPath() error = %v", err)
	}
	if cfg.URL != "http://localhost:1933" {
		t.Fatalf("URL not trimmed: %q", cfg.URL)
	}
	if cfg.APIKey != "secret-1234" {
		t.Fatalf("APIKey mismatch: %q", cfg.APIKey)
	}
	if cfg.EffectiveWorkspace() != "ws_42" {
		t.Fatalf("EffectiveWorkspace mismatch: %q", cfg.EffectiveWorkspace())
	}
	if cfg.Namespace != "team-a" {
		t.Fatalf("Namespace mismatch: %q", cfg.Namespace)
	}
	if cfg.Source != path {
		t.Fatalf("Source not recorded: %q", cfg.Source)
	}
	if !cfg.HasCredentials() {
		t.Fatalf("HasCredentials() = false, want true")
	}
}

func TestLoadFromPathRejectsMalformedJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ovcli.conf")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	_, err := LoadFromPath(path)
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}
	if errors.Is(err, ErrNotFound) {
		t.Fatalf("malformed file should not surface ErrNotFound, got %v", err)
	}
}

func TestEffectiveWorkspaceFallsBackToWorkspaceField(t *testing.T) {
	cfg := Config{Workspace: "legacy-ws"}
	if got := cfg.EffectiveWorkspace(); got != "legacy-ws" {
		t.Fatalf("expected legacy-ws, got %q", got)
	}
	cfg.WorkspaceID = "new-ws"
	if got := cfg.EffectiveWorkspace(); got != "new-ws" {
		t.Fatalf("expected new-ws to win, got %q", got)
	}
}

func TestMaskedAPIKey(t *testing.T) {
	cases := map[string]string{
		"":             "",
		"abc":          "***",
		"abcd":         "****",
		"abcdef":       "**cdef",
		"sk-very-long": "********long",
	}
	for in, want := range cases {
		if got := MaskedAPIKey(in); got != want {
			t.Fatalf("MaskedAPIKey(%q) = %q, want %q", in, got, want)
		}
	}
}

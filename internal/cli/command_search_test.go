package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSearchCommandUsesMemorySearchEndpoint(t *testing.T) {
	root := writeSearchProject(t, "prj_1")
	withWorkingDir(t, root)

	var gotPath string
	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.Query().Get("q")
		if auth := r.Header.Get("Authorization"); auth != "Bearer tok-codex" {
			t.Fatalf("Authorization = %q, want bearer token", auth)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[{"uri":"viking://aitask/projects/prj_1/memory/a.md","title":"A","snippet":"hit"}]}`))
	}))
	defer server.Close()
	saveTestToken(t, server.URL, "tok-codex")

	var stdout bytes.Buffer
	app := NewApp("test")
	app.Stdout = &stdout
	app.Stderr = &bytes.Buffer{}
	app.Stdin = strings.NewReader("")
	if err := app.Execute([]string{"--server", server.URL, "--format", "json", "search", "needle", "--refs-only"}); err != nil {
		t.Fatalf("Execute(search) error: %v", err)
	}
	if gotPath != "/api/projects/prj_1/memory/search" || gotQuery != "needle" {
		t.Fatalf("path/query = %q/%q", gotPath, gotQuery)
	}
	var body map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &body); err != nil {
		t.Fatalf("decode stdout: %v\n%s", err, stdout.String())
	}
	if len(asSlice(body["items"])) != 1 {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func TestSearchCommandFallsBackToLocalRGWhenOpenVikingEmpty(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg not installed")
	}
	root := writeSearchProject(t, "prj_1")
	withWorkingDir(t, root)
	if err := os.WriteFile(filepath.Join(root, "notes.md"), []byte("fallback needle lives here\n"), 0o644); err != nil {
		t.Fatalf("write notes: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	defer server.Close()
	saveTestToken(t, server.URL, "tok-codex")

	var stdout bytes.Buffer
	app := NewApp("test")
	app.Stdout = &stdout
	app.Stderr = &bytes.Buffer{}
	app.Stdin = strings.NewReader("")
	if err := app.Execute([]string{"--server", server.URL, "--format", "json", "search", "needle"}); err != nil {
		t.Fatalf("Execute(search) error: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &body); err != nil {
		t.Fatalf("decode stdout: %v\n%s", err, stdout.String())
	}
	if !mapBool(body, "fallback") || len(asSlice(body["items"])) == 0 {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func writeSearchProject(t *testing.T, projectID string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".aitask"), 0o755); err != nil {
		t.Fatalf("mkdir .aitask: %v", err)
	}
	doc := "# AI Task Project\nproject_id: " + projectID + "\nproject_name: Demo\nopenviking_root: viking://aitask/projects/" + projectID + "\nroom_enabled: true\n"
	if err := os.WriteFile(filepath.Join(root, ".aitask", "project.md"), []byte(doc), 0o644); err != nil {
		t.Fatalf("write project doc: %v", err)
	}
	return root
}

func saveTestToken(t *testing.T, serverURL string, token string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv(tokenStoreFileModeEnv, "file")
	store, err := NewTokenStore()
	if err != nil {
		t.Fatalf("NewTokenStore() error: %v", err)
	}
	if err := store.Save(serverURL, DefaultProfileName, token); err != nil {
		t.Fatalf("Save() error: %v", err)
	}
}

func withWorkingDir(t *testing.T, dir string) {
	t.Helper()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir() error: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })
}

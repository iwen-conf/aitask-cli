package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func TestInitCommandAutoCreatesProjectAndSyncsRepositoryIndex(t *testing.T) {
	root := t.TempDir()
	withWorkingDir(t, root)
	if err := exec.Command("git", "init").Run(); err != nil {
		t.Skipf("git init unavailable: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# Demo\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	_ = exec.Command("git", "-C", root, "add", "README.md").Run()

	var sawCreate, sawPatch, sawMemory bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/projects":
			sawCreate = true
			_, _ = w.Write([]byte(`{"projectId":"prj_auto","name":"Demo","status":"active","activeSessionId":"sess_1","openvikingRoot":"viking://aitask/projects/prj_auto","roomId":"room_1","initCommand":"aitask init --project prj_auto"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/projects/prj_auto":
			_, _ = w.Write([]byte(`{"projectId":"prj_auto","name":"Demo","openvikingRoot":"viking://aitask/projects/prj_auto","openvikingNamespace":"aitask","openvikingWorkspaceId":"","roomId":"room_1"}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/api/projects/prj_auto":
			sawPatch = true
			_, _ = w.Write([]byte(`{"projectId":"prj_auto","name":"Demo","status":"active"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/projects/prj_auto/memory/write":
			sawMemory = true
			_, _ = w.Write([]byte(`{"uri":"viking://user/aitask/projects/prj_auto/memories/resources/repository-index.md","synced":true}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	var stdout bytes.Buffer
	app := NewApp("test")
	app.Stdout = &stdout
	app.Stderr = &bytes.Buffer{}
	app.Stdin = strings.NewReader("")
	if err := app.Execute([]string{"--server", server.URL, "--format", "json", "init", "--name", "Demo"}); err != nil {
		t.Fatalf("Execute(init) error: %v", err)
	}
	if !sawCreate || !sawPatch || !sawMemory {
		t.Fatalf("requests create=%t patch=%t memory=%t", sawCreate, sawPatch, sawMemory)
	}
	body, err := os.ReadFile(filepath.Join(root, ".aitask", "project.md"))
	if err != nil {
		t.Fatalf("read project.md: %v", err)
	}
	text := string(body)
	if !strings.Contains(text, "project_id: prj_auto") || !strings.Contains(text, "openviking_workspace_id: ws_") {
		t.Fatalf("project.md missing expected fields:\n%s", text)
	}
}

func TestProjectSyncWritesRepositoryIndex(t *testing.T) {
	root := writeSearchProject(t, "prj_1")
	withWorkingDir(t, root)
	if err := os.WriteFile(filepath.Join(root, "notes.md"), []byte("sync me\n"), 0o644); err != nil {
		t.Fatalf("write notes: %v", err)
	}

	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/projects/prj_1/memory/write" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"uri":"viking://user/aitask/projects/prj_1/memories/resources/repository-index.md","synced":true}`))
	}))
	defer server.Close()
	saveTestToken(t, server.URL, "tok-codex")

	var stdout bytes.Buffer
	app := NewApp("test")
	app.Stdout = &stdout
	app.Stderr = &bytes.Buffer{}
	app.Stdin = strings.NewReader("")
	if err := app.Execute([]string{"--server", server.URL, "--format", "json", "project", "sync"}); err != nil {
		t.Fatalf("Execute(project sync) error: %v", err)
	}
	if gotBody["target"] != "resources" || gotBody["title"] != "repository-index" {
		t.Fatalf("unexpected memory write body: %#v", gotBody)
	}
	if !strings.Contains(mapString(gotBody, "content"), "# Repository Index") {
		t.Fatalf("content missing repository index: %#v", gotBody)
	}
}

func TestProjectSyncCodeRegistersGitResource(t *testing.T) {
	root := writeSearchProject(t, "prj_1")
	withWorkingDir(t, root)
	if err := exec.Command("git", "-C", root, "init").Run(); err != nil {
		t.Skipf("git init unavailable: %v", err)
	}
	if err := exec.Command("git", "-C", root, "remote", "add", "origin", "git@example.com:org/aitask.git").Run(); err != nil {
		t.Fatalf("git remote add: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# Demo\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	if err := exec.Command("git", "-C", root, "add", "README.md").Run(); err != nil {
		t.Fatalf("git add: %v", err)
	}
	if err := exec.Command("git", "-C", root, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "init").Run(); err != nil {
		t.Fatalf("git commit: %v", err)
	}

	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/projects/prj_1/openviking/resources/git" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer tok-codex" {
			t.Fatalf("Authorization = %q, want bearer token", auth)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"uri":"viking://resources/aitask","repositoryUrl":"git@example.com:org/aitask.git","synced":true}`))
	}))
	defer server.Close()
	saveTestToken(t, server.URL, "tok-codex")

	var stdout bytes.Buffer
	app := NewApp("test")
	app.Stdout = &stdout
	app.Stderr = &bytes.Buffer{}
	app.Stdin = strings.NewReader("")
	if err := app.Execute([]string{"--server", server.URL, "--format", "json", "project", "sync-code"}); err != nil {
		t.Fatalf("Execute(project sync-code) error: %v", err)
	}
	if gotBody["repositoryUrl"] != "git@example.com:org/aitask.git" || gotBody["targetUri"] != "viking://resources/demo" {
		t.Fatalf("unexpected sync-code body: %#v", gotBody)
	}
	if gotBody["watchInterval"] != float64(5) || gotBody["branch"] == "" || gotBody["commit"] == "" {
		t.Fatalf("sync-code body missing watch/git metadata: %#v", gotBody)
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("decode stdout: %v\n%s", err, stdout.String())
	}
	if mapString(out, "repositoryUrl") != "git@example.com:org/aitask.git" || mapString(out, "projectId") != "prj_1" {
		t.Fatalf("unexpected stdout: %s", stdout.String())
	}
}

func TestProjectSyncCodeStatus(t *testing.T) {
	root := writeSearchProject(t, "prj_1")
	withWorkingDir(t, root)

	var gotPath string
	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"targetUri":"viking://resources/aitask","monitored":true,"indexed":true,"status":"running","items":[{"taskId":"task_1","status":"running","taskType":"add_resource"}],"current":{"taskId":"task_1","status":"running","taskType":"add_resource"}}`))
	}))
	defer server.Close()
	saveTestToken(t, server.URL, "tok-codex")

	var stdout bytes.Buffer
	app := NewApp("test")
	app.Stdout = &stdout
	app.Stderr = &bytes.Buffer{}
	app.Stdin = strings.NewReader("")
	if err := app.Execute([]string{"--server", server.URL, "--format", "json", "project", "sync-code", "status", "--to", "viking://resources/aitask", "--limit", "5"}); err != nil {
		t.Fatalf("Execute(project sync-code status) error: %v", err)
	}
	if gotPath != "/api/projects/prj_1/openviking/resources/git/status" {
		t.Fatalf("path = %q, want status endpoint", gotPath)
	}
	values, _ := url.ParseQuery(gotQuery)
	if values.Get("targetUri") != "viking://resources/aitask" || values.Get("limit") != "5" {
		t.Fatalf("query = %q", gotQuery)
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("decode stdout: %v\n%s", err, stdout.String())
	}
	if !mapBool(out, "monitored") || !mapBool(out, "indexed") || mapString(out, "projectId") != "prj_1" {
		t.Fatalf("unexpected stdout: %s", stdout.String())
	}
}

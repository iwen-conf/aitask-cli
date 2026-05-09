package cli

import (
	"bytes"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	localstate "github.com/iwen-conf/aitask-cli/internal/state"
)

func TestSummaryCommandThreadReadsLocalSummary(t *testing.T) {
	home, dbPath := seedSummaryDB(t)
	t.Setenv("HOME", home)
	t.Setenv(localstate.EnvStateDB, dbPath)

	stdout, err := runSummaryTestCommand("--format", "prompt", "summary", "--thread", "thr_1")
	if err != nil {
		t.Fatalf("summary --thread error: %v", err)
	}
	if !strings.Contains(stdout, "Thread summary") {
		t.Fatalf("stdout = %s", stdout)
	}
}

func TestSummaryCommandAgentFallsBackToMemorySearch(t *testing.T) {
	root := writeSearchProject(t, "prj_1")
	withWorkingDir(t, root)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(localstate.EnvStateDB, filepath.Join(home, ".aitask", "missing.db"))

	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("q")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[{"uri":"viking://aitask/projects/prj_1/memory/summary.md","title":"Codex Summary"}]}`))
	}))
	defer server.Close()
	saveTestToken(t, server.URL, "tok-codex")

	stdout, err := runSummaryTestCommand("--server", server.URL, "--format", "json", "summary", "--agent", "codex")
	if err != nil {
		t.Fatalf("summary --agent error: %v", err)
	}
	if gotQuery != "summary agent:codex" {
		t.Fatalf("query = %q", gotQuery)
	}
	if !strings.Contains(stdout, "Codex Summary") {
		t.Fatalf("stdout = %s", stdout)
	}
}

func TestSummaryCommandProjectNoSummaryRecorded(t *testing.T) {
	root := writeSearchProject(t, "prj_1")
	withWorkingDir(t, root)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(localstate.EnvStateDB, filepath.Join(home, ".aitask", "missing.db"))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	defer server.Close()
	saveTestToken(t, server.URL, "tok-codex")

	stdout, err := runSummaryTestCommand("--server", server.URL, "--format", "prompt", "summary", "--project")
	if err != nil {
		t.Fatalf("summary --project error: %v", err)
	}
	if !strings.Contains(stdout, "No summary recorded") {
		t.Fatalf("stdout = %s", stdout)
	}
}

func runSummaryTestCommand(args ...string) (string, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp("test")
	app.Stdout = &stdout
	app.Stderr = &stderr
	app.Stdin = strings.NewReader("")
	err := app.Execute(args)
	return stdout.String(), err
}

func seedSummaryDB(t *testing.T) (string, string) {
	t.Helper()
	home := t.TempDir()
	dbPath := filepath.Join(home, ".aitask", "state.db")
	db, closeDB, err := localstate.OpenPath(t.Context(), dbPath)
	if err != nil {
		t.Fatalf("OpenPath() error: %v", err)
	}
	defer closeDB()
	if err := localstate.Migrate(t.Context(), db); err != nil {
		t.Fatalf("Migrate() error: %v", err)
	}
	insertSummary(t, db, "thread:thr_1", "thread", "thr_1", "Thread summary")
	return home, dbPath
}

func insertSummary(t *testing.T, db *sql.DB, id, scope, scopeID, summary string) {
	t.Helper()
	if _, err := db.ExecContext(t.Context(), `INSERT INTO summaries(id, scope, scope_id, summary, updated_at)
VALUES (?, ?, ?, ?, ?)`, id, scope, scopeID, summary, "2026-05-08T00:00:00Z"); err != nil {
		t.Fatalf("insert summary: %v", err)
	}
}

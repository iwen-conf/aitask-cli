package cli

import (
	"bytes"
	"database/sql"
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

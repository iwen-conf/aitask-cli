package cli

import (
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	localstate "github.com/iwen-conf/aitask-cli/internal/state"
)

func TestWorkerCommandOnceMemoryNone(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("AITASK_STATE_DB", filepath.Join(home, ".aitask", "state.db"))
	if err := os.MkdirAll(filepath.Join(home, ".aitask"), 0o700); err != nil {
		t.Fatalf("mkdir .aitask: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".aitask", "events.ndjson"), []byte(`{"kind":"mention","ts":"2026-05-07T12:00:00Z","project":"prj_1","eventId":"evt_1","messageId":"thr_1","from":{"agentType":"claude-code"},"content":"@codex handle","mentions":["codex"]}
`), 0o600); err != nil {
		t.Fatalf("write events: %v", err)
	}
	app := NewApp("test")
	var stdout, stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr
	err := app.Execute([]string{"--format", "brief", "worker", "--once", "--memory", "none", "--quiet"})
	if err != nil {
		t.Fatalf("Execute() error: %v; stderr=%s", err, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) == "" {
		t.Fatalf("worker output is empty")
	}
}

func TestWorkerCommandAcceptsOpenVikingMemoryMode(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("AITASK_STATE_DB", filepath.Join(home, ".aitask", "state.db"))
	if err := os.MkdirAll(filepath.Join(home, ".aitask"), 0o700); err != nil {
		t.Fatalf("mkdir .aitask: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".aitask", "events.ndjson"), []byte(`{"kind":"ready","ts":"2026-05-07T12:00:00Z","project":"prj_1","eventId":"evt_1"}
`), 0o600); err != nil {
		t.Fatalf("write events: %v", err)
	}
	root := writeSearchProject(t, "prj_1")
	withWorkingDir(t, root)
	t.Setenv(tokenStoreFileModeEnv, "file")
	store, err := NewTokenStore()
	if err != nil {
		t.Fatalf("NewTokenStore() error: %v", err)
	}
	if err := store.Save("http://127.0.0.1:1", DefaultProfileName, "tok-codex"); err != nil {
		t.Fatalf("Save() error: %v", err)
	}
	stdout, stderr, err := runWorkerCommand(t, home, filepath.Join(home, ".aitask", "state.db"), "--server", "http://127.0.0.1:1", "--format", "brief", "worker", "--once", "--memory", "openviking", "--quiet")
	if err != nil {
		t.Fatalf("worker --memory openviking error: %v; stderr=%s", err, stderr)
	}
	if strings.TrimSpace(stdout) == "" {
		t.Fatalf("worker output is empty")
	}
}

func TestWorkerCommandBackfillSinceIsIdempotent(t *testing.T) {
	home, dbPath := seedWorkerBackfillDB(t)
	stdout, stderr, err := runWorkerCommand(t, home, dbPath, "--format", "json", "worker", "--backfill-since", "2026-05-08T00:00:00Z", "--quiet")
	if err != nil {
		t.Fatalf("first backfill error: %v; stderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"inserted": 1`) {
		t.Fatalf("stdout = %s", stdout)
	}
	stdout, stderr, err = runWorkerCommand(t, home, dbPath, "--format", "json", "worker", "--backfill-since", "2026-05-08T00:00:00Z", "--quiet")
	if err != nil {
		t.Fatalf("second backfill error: %v; stderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"inserted": 0`) {
		t.Fatalf("stdout = %s", stdout)
	}
	assertWorkerCommandCount(t, dbPath, `SELECT COUNT(*) FROM memory_sync`, 1)
}

func TestWorkerCommandBackfillDryRunDoesNotWrite(t *testing.T) {
	home, dbPath := seedWorkerBackfillDB(t)
	stdout, stderr, err := runWorkerCommand(t, home, dbPath, "--format", "json", "worker", "--backfill-since", "2026-05-08T00:00:00Z", "--dry-run", "--quiet")
	if err != nil {
		t.Fatalf("dry-run backfill error: %v; stderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"dryRun": true`) || !strings.Contains(stdout, `"inserted": 0`) {
		t.Fatalf("stdout = %s", stdout)
	}
	assertWorkerCommandCount(t, dbPath, `SELECT COUNT(*) FROM memory_sync`, 0)
}

func runWorkerCommand(t *testing.T, home, dbPath string, args ...string) (string, string, error) {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv(localstate.EnvStateDB, dbPath)
	app := NewApp("test")
	var stdout, stderr bytes.Buffer
	app.Stdout = &stdout
	app.Stderr = &stderr
	app.Stdin = strings.NewReader("")
	err := app.Execute(args)
	return stdout.String(), stderr.String(), err
}

func seedWorkerBackfillDB(t *testing.T) (string, string) {
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
	insertWorkerCommandEvent(t, db, "evt_1", "task.submitted", "2026-05-08T00:00:00Z")
	insertWorkerCommandEvent(t, db, "evt_2", "daemon", "2026-05-08T00:01:00Z")
	return home, dbPath
}

func insertWorkerCommandEvent(t *testing.T, db *sql.DB, id, kind, createdAt string) {
	t.Helper()
	if _, err := db.ExecContext(t.Context(), `INSERT INTO events(id, kind, raw_json, created_at, indexed_at)
VALUES (?, ?, '{}', ?, ?)`, id, kind, createdAt, createdAt); err != nil {
		t.Fatalf("insert event %s: %v", id, err)
	}
}

func assertWorkerCommandCount(t *testing.T, dbPath, query string, want int) {
	t.Helper()
	db, closeDB, err := localstate.OpenPath(t.Context(), dbPath)
	if err != nil {
		t.Fatalf("OpenPath() error: %v", err)
	}
	defer closeDB()
	var got int
	if err := db.QueryRowContext(t.Context(), query).Scan(&got); err != nil {
		t.Fatalf("count query failed: %v", err)
	}
	if got != want {
		t.Fatalf("%s = %d, want %d", query, got, want)
	}
}

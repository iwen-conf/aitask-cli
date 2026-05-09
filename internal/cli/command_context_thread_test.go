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

func TestContextThreadCommandRendersSummaryAndEventsInOrder(t *testing.T) {
	home := t.TempDir()
	dbPath := filepath.Join(home, ".aitask", "state.db")
	t.Setenv("HOME", home)
	t.Setenv(localstate.EnvStateDB, dbPath)
	db, closeDB, err := localstate.OpenPath(t.Context(), dbPath)
	if err != nil {
		t.Fatalf("OpenPath() error: %v", err)
	}
	defer closeDB()
	if err := localstate.Migrate(t.Context(), db); err != nil {
		t.Fatalf("Migrate() error: %v", err)
	}
	insertContextThreadEvent(t, db, "evt_1", "thr_1", "first", "2026-05-08T00:00:00Z")
	insertContextThreadEvent(t, db, "evt_2", "thr_1", "second", "2026-05-08T00:01:00Z")
	insertSummary(t, db, "thread:thr_1", "thread", "thr_1", "Thread summary")

	var stdout, stderr bytes.Buffer
	app := NewApp("test")
	app.Stdout = &stdout
	app.Stderr = &stderr
	app.Stdin = strings.NewReader("")
	if err := app.Execute([]string{"--format", "prompt", "context", "--thread", "thr_1"}); err != nil {
		t.Fatalf("context --thread error: %v; stderr=%s", err, stderr.String())
	}
	output := stdout.String()
	first := strings.Index(output, "first")
	second := strings.Index(output, "second")
	if !strings.Contains(output, "Thread summary") || first < 0 || second < 0 || first > second {
		t.Fatalf("stdout = %s", output)
	}
}

func insertContextThreadEvent(t *testing.T, db *sql.DB, id, threadID, body, createdAt string) {
	t.Helper()
	if _, err := db.ExecContext(t.Context(), `INSERT INTO events(id, kind, thread_id, from_agent, body, raw_json, created_at, indexed_at)
VALUES (?, 'mention', ?, 'claude-code', ?, '{}', ?, ?)`, id, threadID, body, createdAt, createdAt); err != nil {
		t.Fatalf("insert event %s: %v", id, err)
	}
}

func TestContextThreadSubcommandAlsoWorks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(localstate.EnvStateDB, filepath.Join(home, ".aitask", "missing.db"))
	if err := os.MkdirAll(filepath.Join(home, ".aitask"), 0o755); err != nil {
		t.Fatalf("mkdir .aitask: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".aitask", "events.ndjson"), []byte(`{"kind":"mention","ts":"2026-05-08T00:00:00Z","eventId":"evt_1","messageId":"thr_1","content":"hello"}
`), 0o644); err != nil {
		t.Fatalf("write events: %v", err)
	}
	var stdout bytes.Buffer
	app := NewApp("test")
	app.Stdout = &stdout
	app.Stderr = &bytes.Buffer{}
	app.Stdin = strings.NewReader("")
	if err := app.Execute([]string{"--format", "json", "context", "thread", "thr_1"}); err != nil {
		t.Fatalf("context thread error: %v", err)
	}
	if !strings.Contains(stdout.String(), `"threadId": "thr_1"`) {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func TestContextEventCommandRendersEventAndThreadSummary(t *testing.T) {
	home := t.TempDir()
	dbPath := filepath.Join(home, ".aitask", "state.db")
	t.Setenv("HOME", home)
	t.Setenv(localstate.EnvStateDB, dbPath)
	db, closeDB, err := localstate.OpenPath(t.Context(), dbPath)
	if err != nil {
		t.Fatalf("OpenPath() error: %v", err)
	}
	defer closeDB()
	if err := localstate.Migrate(t.Context(), db); err != nil {
		t.Fatalf("Migrate() error: %v", err)
	}
	insertContextThreadEvent(t, db, "evt_1", "thr_1", "event body", "2026-05-08T00:00:00Z")
	insertSummary(t, db, "thread:thr_1", "thread", "thr_1", "Thread summary")

	var stdout, stderr bytes.Buffer
	app := NewApp("test")
	app.Stdout = &stdout
	app.Stderr = &stderr
	app.Stdin = strings.NewReader("")
	if err := app.Execute([]string{"--format", "prompt", "context", "--event", "evt_1"}); err != nil {
		t.Fatalf("context --event error: %v; stderr=%s", err, stderr.String())
	}
	output := stdout.String()
	if !strings.Contains(output, "# Event Context") || !strings.Contains(output, "event body") || !strings.Contains(output, "Thread summary") {
		t.Fatalf("stdout = %s", output)
	}
}

func TestContextEventSubcommandAlsoWorks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(localstate.EnvStateDB, filepath.Join(home, ".aitask", "missing.db"))
	if err := os.MkdirAll(filepath.Join(home, ".aitask"), 0o755); err != nil {
		t.Fatalf("mkdir .aitask: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".aitask", "events.ndjson"), []byte(`{"kind":"mention","ts":"2026-05-08T00:00:00Z","eventId":"evt_1","messageId":"thr_1","content":"hello"}
`), 0o644); err != nil {
		t.Fatalf("write events: %v", err)
	}
	var stdout bytes.Buffer
	app := NewApp("test")
	app.Stdout = &stdout
	app.Stderr = &bytes.Buffer{}
	app.Stdin = strings.NewReader("")
	if err := app.Execute([]string{"--format", "json", "context", "event", "evt_1"}); err != nil {
		t.Fatalf("context event error: %v", err)
	}
	if !strings.Contains(stdout.String(), `"eventId": "evt_1"`) {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

package worker

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	localstate "github.com/iwen-conf/aitask-cli/internal/state"
)

func TestRunOnceIngestsAndSummarizes(t *testing.T) {
	db := newTestDB(t)
	path := writeNDJSON(t, `{"kind":"mention","ts":"2026-05-07T12:00:00Z","project":"prj_1","eventId":"evt_1","messageId":"thr_1","from":{"agentType":"claude-code"},"content":"@codex handle this","mentions":["codex"]}
{"kind":"broadcast","ts":"2026-05-07T12:01:00Z","project":"prj_1","eventId":"evt_2","messageId":"thr_1","content":"global note"}
`)
	stats, err := RunOnce(context.Background(), Options{
		StateDB:    db,
		NDJSONPath: path,
		Logger:     log.New(os.Stderr, "test", 0),
	})
	if err != nil {
		t.Fatalf("RunOnce() error: %v", err)
	}
	if stats.Ingested != 2 || stats.RoutedAgent != 1 || stats.RoutedGlobal != 1 || stats.SummariesUpdated != 1 {
		t.Fatalf("stats = %#v", stats)
	}
	assertCount(t, db, `SELECT COUNT(*) FROM summaries WHERE scope='thread' AND scope_id='thr_1'`, 1)
}

func TestRunOnceReplayIsIdempotent(t *testing.T) {
	db := newTestDB(t)
	path := writeNDJSON(t, `{"kind":"mention","ts":"2026-05-07T12:00:00Z","project":"prj_1","eventId":"evt_1","messageId":"thr_1","from":{"agentType":"claude-code"},"content":"@codex handle","mentions":["codex"]}
`)
	opts := Options{StateDB: db, NDJSONPath: path, Logger: log.New(os.Stderr, "test", 0)}
	first, err := RunOnce(context.Background(), opts)
	if err != nil {
		t.Fatalf("first RunOnce() error: %v", err)
	}
	second, err := RunOnce(context.Background(), opts)
	if err != nil {
		t.Fatalf("second RunOnce() error: %v", err)
	}
	if first.Ingested != 1 || second.Ingested != 0 {
		t.Fatalf("first=%#v second=%#v", first, second)
	}
	assertCount(t, db, `SELECT COUNT(*) FROM events`, 1)
	assertCount(t, db, `SELECT COUNT(*) FROM agent_inbox`, 1)
}

func TestRunDaemonStopsOnCancel(t *testing.T) {
	db := newTestDB(t)
	path := writeNDJSON(t, `{"kind":"mention","ts":"2026-05-07T12:00:00Z","project":"prj_1","eventId":"evt_1","messageId":"thr_1","from":{"agentType":"claude-code"},"content":"@codex handle","mentions":["codex"]}
`)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- RunDaemon(ctx, Options{StateDB: db, NDJSONPath: path, Interval: time.Millisecond, Logger: log.New(os.Stderr, "test", 0)})
	}()
	time.Sleep(2200 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("RunDaemon() error = %v, want context.Canceled", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("RunDaemon() did not stop after cancel")
	}
}

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, closeDB, err := localstate.OpenPath(context.Background(), filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("OpenPath() error: %v", err)
	}
	t.Cleanup(func() { _ = closeDB() })
	if err := localstate.Migrate(context.Background(), db); err != nil {
		t.Fatalf("Migrate() error: %v", err)
	}
	return db
}

func writeNDJSON(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "events.ndjson")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write ndjson: %v", err)
	}
	return path
}

func assertCount(t *testing.T, db *sql.DB, query string, want int) {
	t.Helper()
	var got int
	if err := db.QueryRow(query).Scan(&got); err != nil {
		t.Fatalf("count query failed: %v", err)
	}
	if got != want {
		t.Fatalf("%s = %d, want %d", query, got, want)
	}
}

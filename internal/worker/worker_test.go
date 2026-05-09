package worker

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	localstate "github.com/iwen-conf/aitask-cli/internal/state"
)

type fakeSyncer struct {
	err   error
	calls []WriteMemoryRequest
}

func (f *fakeSyncer) WriteMemory(_ context.Context, req WriteMemoryRequest) (WriteMemoryResponse, error) {
	f.calls = append(f.calls, req)
	if f.err != nil {
		return WriteMemoryResponse{}, f.err
	}
	return WriteMemoryResponse{URI: "viking://memory/" + req.RelatedEventID, MemoryID: "mem_" + req.RelatedEventID}, nil
}

func TestRunOnceIngestsSyncsAndSummarizes(t *testing.T) {
	db := newTestDB(t)
	path := writeNDJSON(t, `{"kind":"mention","ts":"2026-05-07T12:00:00Z","project":"prj_1","eventId":"evt_1","messageId":"thr_1","from":{"agentType":"claude-code"},"content":"@codex handle this","mentions":["codex"]}
{"kind":"broadcast","ts":"2026-05-07T12:01:00Z","project":"prj_1","eventId":"evt_2","messageId":"thr_1","content":"global note"}
`)
	syncer := &fakeSyncer{}
	stats, err := RunOnce(context.Background(), Options{
		StateDB:    db,
		NDJSONPath: path,
		Sync:       syncer,
		Logger:     log.New(os.Stderr, "test", 0),
		MaxRetries: 5,
	})
	if err != nil {
		t.Fatalf("RunOnce() error: %v", err)
	}
	if stats.Ingested != 2 || stats.RoutedAgent != 1 || stats.RoutedGlobal != 1 || stats.SyncSucceeded != 2 || stats.SummariesUpdated != 1 {
		t.Fatalf("stats = %#v", stats)
	}
	if len(syncer.calls) != 2 {
		t.Fatalf("sync calls = %d, want 2", len(syncer.calls))
	}
	if syncer.calls[0].RelatedEventID != "evt_1" || !strings.Contains(syncer.calls[0].Content, "event:evt_1") {
		t.Fatalf("first sync call = %#v", syncer.calls[0])
	}
	assertCount(t, db, `SELECT COUNT(*) FROM memory_sync WHERE status='synced'`, 2)
	assertCount(t, db, `SELECT COUNT(*) FROM summaries WHERE scope='thread' AND scope_id='thr_1'`, 1)
}

func TestRunOnceSyncFailureDoesNotBlockIngest(t *testing.T) {
	db := newTestDB(t)
	path := writeNDJSON(t, `{"kind":"mention","ts":"2026-05-07T12:00:00Z","project":"prj_1","eventId":"evt_1","messageId":"thr_1","from":{"agentType":"claude-code"},"content":"@codex handle","mentions":["codex"]}
`)
	stats, err := RunOnce(context.Background(), Options{
		StateDB:    db,
		NDJSONPath: path,
		Sync:       &fakeSyncer{err: errors.New("backend down")},
		Logger:     log.New(os.Stderr, "test", 0),
		MaxRetries: 5,
	})
	if err != nil {
		t.Fatalf("RunOnce() error: %v", err)
	}
	if stats.Ingested != 1 || stats.SyncFailed != 1 {
		t.Fatalf("stats = %#v", stats)
	}
	assertCount(t, db, `SELECT COUNT(*) FROM events`, 1)
	assertCount(t, db, `SELECT COUNT(*) FROM memory_sync WHERE status='failed' AND retry_count=1`, 1)
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
	assertCount(t, db, `SELECT COUNT(*) FROM memory_sync`, 1)
}

func TestRunOnceSkipsAfterMaxRetries(t *testing.T) {
	db := newTestDB(t)
	path := writeNDJSON(t, `{"kind":"mention","ts":"2026-05-07T12:00:00Z","project":"prj_1","eventId":"evt_1","messageId":"thr_1","from":{"agentType":"claude-code"},"content":"@codex handle","mentions":["codex"]}
`)
	opts := Options{
		StateDB:    db,
		NDJSONPath: path,
		Sync:       &fakeSyncer{err: errors.New("backend down")},
		Logger:     log.New(os.Stderr, "test", 0),
		MaxRetries: 2,
	}
	if _, err := RunOnce(context.Background(), opts); err != nil {
		t.Fatalf("first RunOnce() error: %v", err)
	}
	if _, err := RunOnce(context.Background(), opts); err != nil {
		t.Fatalf("second RunOnce() error: %v", err)
	}
	assertCount(t, db, `SELECT COUNT(*) FROM memory_sync WHERE status='skipped' AND retry_count=2`, 1)
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

func TestSyncableKindWhitelistMatchesServerEvents(t *testing.T) {
	syncable := []string{
		"mention",
		"task.submitted",
		"task.failed",
		"task.review_passed",
		"task.review_rejected",
		"task.reviewed",
		"room.decision_pinned",
		"system_event",
	}
	for _, kind := range syncable {
		if !syncableKind(kind) {
			t.Fatalf("syncableKind(%q) = false, want true", kind)
		}
	}
	ignored := []string{"ready", "daemon", "error", "room.member_online", "heartbeat"}
	for _, kind := range ignored {
		if syncableKind(kind) {
			t.Fatalf("syncableKind(%q) = true, want false", kind)
		}
	}
}

func TestBackfillMemorySyncIsIdempotent(t *testing.T) {
	db := newTestDB(t)
	insertBackfillEvent(t, db, "evt_1", "mention", "2026-05-08T00:00:00Z")
	insertBackfillEvent(t, db, "evt_2", "ready", "2026-05-08T00:01:00Z")
	since := time.Date(2026, 5, 8, 0, 0, 0, 0, time.UTC)
	first, err := BackfillMemorySync(context.Background(), BackfillOptions{StateDB: db, Since: since})
	if err != nil {
		t.Fatalf("first BackfillMemorySync() error: %v", err)
	}
	second, err := BackfillMemorySync(context.Background(), BackfillOptions{StateDB: db, Since: since})
	if err != nil {
		t.Fatalf("second BackfillMemorySync() error: %v", err)
	}
	if first.Matched != 1 || first.Inserted != 1 || second.Matched != 0 || second.Inserted != 0 {
		t.Fatalf("first=%#v second=%#v", first, second)
	}
	assertCount(t, db, `SELECT COUNT(*) FROM memory_sync`, 1)
}

func TestBackfillMemorySyncDryRunDoesNotWrite(t *testing.T) {
	db := newTestDB(t)
	insertBackfillEvent(t, db, "evt_1", "mention", "2026-05-08T00:00:00Z")
	stats, err := BackfillMemorySync(context.Background(), BackfillOptions{
		StateDB: db,
		Since:   time.Date(2026, 5, 8, 0, 0, 0, 0, time.UTC),
		DryRun:  true,
	})
	if err != nil {
		t.Fatalf("BackfillMemorySync() error: %v", err)
	}
	if stats.Matched != 1 || stats.Inserted != 0 || !stats.DryRun || len(stats.EventIDs) != 1 {
		t.Fatalf("stats = %#v", stats)
	}
	assertCount(t, db, `SELECT COUNT(*) FROM memory_sync`, 0)
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

func insertBackfillEvent(t *testing.T, db *sql.DB, id, kind, createdAt string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO events(id, kind, raw_json, created_at, indexed_at)
VALUES (?, ?, '{}', ?, ?)`, id, kind, createdAt, createdAt); err != nil {
		t.Fatalf("insert event %s: %v", id, err)
	}
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

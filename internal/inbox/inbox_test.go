package inbox

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	localstate "github.com/iwen-conf/aitask-cli/internal/state"
)

func TestIngestAndListAgentInbox(t *testing.T) {
	db := newTestDB(t)
	path := writeNDJSON(t, `{"kind":"mention","ts":"2026-05-07T12:00:00Z","project":"prj_1","eventId":"evt_1","messageId":"msg_1","from":{"agentType":"claude-code"},"content":"@codex handle","mentions":["codex"]}
{"kind":"mention","ts":"2026-05-07T12:01:00Z","project":"prj_1","eventId":"evt_2","messageId":"msg_2","from":{"agentType":"codex"},"content":"@codex self","mentions":["codex"]}
`)
	if err := Ingest(context.Background(), db, path); err != nil {
		t.Fatalf("Ingest() error: %v", err)
	}
	rows, err := ListAgentInbox(context.Background(), db, "codex", ListOpts{})
	if err != nil {
		t.Fatalf("ListAgentInbox() error: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != "evt_1" {
		t.Fatalf("rows = %#v, want only evt_1", rows)
	}
}

func TestAckTwiceReturnsErrNotApplicable(t *testing.T) {
	db := newTestDB(t)
	path := writeNDJSON(t, `{"kind":"mention","ts":"2026-05-07T12:00:00Z","project":"prj_1","eventId":"evt_1","from":{"agentType":"claude-code"},"content":"@codex handle","mentions":["codex"]}
`)
	if err := Ingest(context.Background(), db, path); err != nil {
		t.Fatalf("Ingest() error: %v", err)
	}
	if _, err := Ack(context.Background(), db, "evt_1", "codex"); err != nil {
		t.Fatalf("first Ack() error: %v", err)
	}
	if _, err := Ack(context.Background(), db, "evt_1", "codex"); !errors.Is(err, ErrNotApplicable) {
		t.Fatalf("second Ack() error = %v, want ErrNotApplicable", err)
	}
}

func TestMissingEventReturnsErrNotApplicable(t *testing.T) {
	db := newTestDB(t)
	if _, err := Ack(context.Background(), db, "missing", "codex"); !errors.Is(err, ErrNotApplicable) {
		t.Fatalf("Ack(missing) error = %v, want ErrNotApplicable", err)
	}
}

func TestGlobalAndLatestAndThread(t *testing.T) {
	db := newTestDB(t)
	path := writeNDJSON(t, `{"kind":"broadcast","ts":"2026-05-07T12:00:00Z","project":"prj_1","eventId":"evt_1","content":"all"}
{"kind":"mention","ts":"2026-05-07T12:01:00Z","project":"prj_1","eventId":"evt_2","messageId":"thread_1","from":{"agentType":"claude-code"},"content":"@codex handle","mentions":["codex"]}
{"kind":"broadcast","ts":"2026-05-07T12:02:00Z","project":"prj_2","eventId":"evt_3","content":"other"}
`)
	if err := Ingest(context.Background(), db, path); err != nil {
		t.Fatalf("Ingest() error: %v", err)
	}
	global, err := ListGlobalFeed(context.Background(), db, ListOpts{Project: "prj_1"})
	if err != nil {
		t.Fatalf("ListGlobalFeed() error: %v", err)
	}
	if len(global) != 2 || global[0].ID != "evt_3" || global[1].ID != "evt_1" {
		t.Fatalf("global = %#v, want evt_3, evt_1", global)
	}
	latest, err := ListLatest(context.Background(), db, 2)
	if err != nil {
		t.Fatalf("ListLatest() error: %v", err)
	}
	if len(latest) != 2 || latest[0].ID != "evt_3" || latest[1].ID != "evt_2" {
		t.Fatalf("latest = %#v, want evt_3, evt_2", latest)
	}
	thread, err := ListThread(context.Background(), db, "thread_1")
	if err != nil {
		t.Fatalf("ListThread() error: %v", err)
	}
	if len(thread) != 1 || thread[0].ID != "evt_2" {
		t.Fatalf("thread = %#v, want evt_2", thread)
	}
}

func TestSelfMentionExcludedWhenFromCarriesUUID(t *testing.T) {
	db := newTestDB(t)
	path := writeNDJSON(t, `{"kind":"mention","ts":"2026-05-07T12:00:00Z","project":"prj_1","eventId":"evt_1","from":{"type":"agent","agentType":"codex","agentId":"agt_xxx"},"content":"@codex self","mentions":["codex"]}
`)
	if err := Ingest(context.Background(), db, path); err != nil {
		t.Fatalf("Ingest() error: %v", err)
	}
	rows, err := ListAgentInbox(context.Background(), db, "codex", ListOpts{})
	if err != nil {
		t.Fatalf("ListAgentInbox() error: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("rows = %#v, want empty (self-mention via agentType=codex must be excluded even when agentId UUID present)", rows)
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

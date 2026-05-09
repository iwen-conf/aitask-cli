package agentwatch

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

	localinbox "github.com/iwen-conf/aitask-cli/internal/inbox"
	localstate "github.com/iwen-conf/aitask-cli/internal/state"
)

type stubRunner struct {
	result RunResult
	err    error
	calls  int
	last   string
}

func (r *stubRunner) Run(_ context.Context, prompt string) (RunResult, error) {
	r.calls++
	r.last = prompt
	return r.result, r.err
}

type stubRecaller struct {
	value string
	err   error
	calls int
}

func (r *stubRecaller) Recall(_ context.Context, _, _ string) (string, error) {
	r.calls++
	return r.value, r.err
}

func TestRunOnceHappyPath(t *testing.T) {
	db := newTestDB(t)
	seedEvents(t, db, `{"kind":"mention","ts":"2026-05-07T12:00:00Z","project":"prj_1","eventId":"evt_1","messageId":"thr_1","from":{"agentType":"claude-code"},"content":"@codex handle","mentions":["codex"]}
`)
	runner := &stubRunner{result: RunResult{Stdout: "ok", ExitCode: 0}}
	stats, err := RunOnce(context.Background(), Options{StateDB: db, Agent: "codex", Runner: runner, Logger: testLogger()})
	if err != nil {
		t.Fatalf("RunOnce() error: %v", err)
	}
	if stats.Picked != 1 || stats.Acked != 1 || stats.Done != 1 || runner.calls != 1 {
		t.Fatalf("stats=%#v calls=%d", stats, runner.calls)
	}
	assertCount(t, db, `SELECT COUNT(*) FROM agent_inbox WHERE event_id='evt_1' AND status='handled'`, 1)
	assertCount(t, db, `SELECT COUNT(*) FROM events WHERE id='evt_1:result:codex' AND kind='task_done' AND body='ok'`, 1)
	assertCount(t, db, `SELECT COUNT(*) FROM memory_sync WHERE event_id='evt_1:result:codex' AND status='pending'`, 1)
}

func TestRunOnceRunnerFailureMarksFailed(t *testing.T) {
	db := newTestDB(t)
	seedEvents(t, db, `{"kind":"mention","ts":"2026-05-07T12:00:00Z","project":"prj_1","eventId":"evt_1","from":{"agentType":"claude-code"},"content":"@codex handle","mentions":["codex"]}
`)
	runner := &stubRunner{result: RunResult{Stderr: "boom", ExitCode: 2}}
	stats, err := RunOnce(context.Background(), Options{StateDB: db, Agent: "codex", Runner: runner, Logger: testLogger(), MaxRetries: 5})
	if err != nil {
		t.Fatalf("RunOnce() error: %v", err)
	}
	if stats.Failed != 1 {
		t.Fatalf("stats=%#v, want failed=1", stats)
	}
	assertCount(t, db, `SELECT COUNT(*) FROM agent_inbox WHERE event_id='evt_1' AND status='failed' AND retry_count=1 AND last_error='boom'`, 1)
}

func TestRunOnceMaxRetriesSkips(t *testing.T) {
	db := newTestDB(t)
	seedEvents(t, db, `{"kind":"mention","ts":"2026-05-07T12:00:00Z","project":"prj_1","eventId":"evt_1","from":{"agentType":"claude-code"},"content":"@codex handle","mentions":["codex"]}
`)
	if _, err := localinbox.Ack(context.Background(), db, "evt_1", "codex"); err != nil {
		t.Fatalf("Ack() error: %v", err)
	}
	if _, err := localinbox.Fail(context.Background(), db, "evt_1", "codex", "first"); err != nil {
		t.Fatalf("Fail() error: %v", err)
	}
	if _, err := db.Exec(`UPDATE agent_inbox SET status='acked', acked_at='2000-01-01T00:00:00Z' WHERE event_id='evt_1' AND agent='codex'`); err != nil {
		t.Fatalf("reset acked: %v", err)
	}
	runner := &stubRunner{result: RunResult{Stderr: "again", ExitCode: 1}}
	stats, err := RunOnce(context.Background(), Options{StateDB: db, Agent: "codex", Runner: runner, Logger: testLogger(), MaxRetries: 2})
	if err != nil {
		t.Fatalf("RunOnce() error: %v", err)
	}
	if stats.Skipped != 1 {
		t.Fatalf("stats=%#v, want skipped=1", stats)
	}
	assertCount(t, db, `SELECT COUNT(*) FROM agent_inbox WHERE event_id='evt_1' AND status='skipped' AND retry_count=2`, 1)
}

func TestSelfMentionNotPicked(t *testing.T) {
	db := newTestDB(t)
	seedEvents(t, db, `{"kind":"mention","ts":"2026-05-07T12:00:00Z","project":"prj_1","eventId":"evt_1","from":{"agentType":"codex"},"content":"@codex self","mentions":["codex"]}
`)
	runner := &stubRunner{result: RunResult{ExitCode: 0}}
	stats, err := RunOnce(context.Background(), Options{StateDB: db, Agent: "codex", Runner: runner, Logger: testLogger()})
	if err != nil {
		t.Fatalf("RunOnce() error: %v", err)
	}
	if stats.Picked != 0 || runner.calls != 0 {
		t.Fatalf("stats=%#v calls=%d, want no pickup", stats, runner.calls)
	}
}

func TestDryRunDoesNotChangeStatus(t *testing.T) {
	db := newTestDB(t)
	seedEvents(t, db, `{"kind":"mention","ts":"2026-05-07T12:00:00Z","project":"prj_1","eventId":"evt_1","from":{"agentType":"claude-code"},"content":"@codex inspect","mentions":["codex"]}
`)
	var out strings.Builder
	stats, err := RunOnce(context.Background(), Options{StateDB: db, Agent: "codex", DryRun: true, PromptWriter: &out, Logger: testLogger()})
	if err != nil {
		t.Fatalf("RunOnce() error: %v", err)
	}
	if stats.Picked != 1 || !strings.Contains(out.String(), "@codex inspect") {
		t.Fatalf("stats=%#v prompt=%q", stats, out.String())
	}
	assertCount(t, db, `SELECT COUNT(*) FROM agent_inbox WHERE event_id='evt_1' AND status='unread'`, 1)
}

func TestRunnerTimeoutMarksFailed(t *testing.T) {
	db := newTestDB(t)
	seedEvents(t, db, `{"kind":"mention","ts":"2026-05-07T12:00:00Z","project":"prj_1","eventId":"evt_1","from":{"agentType":"claude-code"},"content":"@codex handle","mentions":["codex"]}
`)
	runner := runnerFunc(func(ctx context.Context, prompt string) (RunResult, error) {
		<-ctx.Done()
		return RunResult{}, ctx.Err()
	})
	stats, err := RunOnce(context.Background(), Options{StateDB: db, Agent: "codex", Runner: runner, Logger: testLogger(), Timeout: time.Millisecond, MaxRetries: 5})
	if err != nil {
		t.Fatalf("RunOnce() error: %v", err)
	}
	if stats.Failed != 1 {
		t.Fatalf("stats=%#v, want failed=1", stats)
	}
	assertCount(t, db, `SELECT COUNT(*) FROM agent_inbox WHERE event_id='evt_1' AND status='failed' AND retry_count=1`, 1)
}

type runnerFunc func(context.Context, string) (RunResult, error)

func (f runnerFunc) Run(ctx context.Context, prompt string) (RunResult, error) {
	return f(ctx, prompt)
}

func TestRecallFailureFallsBackToNoRecall(t *testing.T) {
	db := newTestDB(t)
	seedEvents(t, db, `{"kind":"mention","ts":"2026-05-07T12:00:00Z","project":"prj_1","eventId":"evt_1","from":{"agentType":"claude-code"},"content":"@codex handle","mentions":["codex"]}
`)
	recaller := &stubRecaller{err: errors.New("backend down")}
	runner := &stubRunner{result: RunResult{ExitCode: 0}}
	stats, err := RunOnce(context.Background(), Options{StateDB: db, Agent: "codex", Runner: runner, ContextRecaller: recaller, Logger: testLogger()})
	if err != nil {
		t.Fatalf("RunOnce() error: %v", err)
	}
	if stats.Done != 1 || recaller.calls != 1 || !strings.Contains(runner.last, "Relevant Context:\n(none)") {
		t.Fatalf("stats=%#v recall=%d prompt=%s", stats, recaller.calls, runner.last)
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

func seedEvents(t *testing.T, db *sql.DB, ndjson string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "events.ndjson")
	if err := os.WriteFile(path, []byte(ndjson), 0o644); err != nil {
		t.Fatalf("write events: %v", err)
	}
	if err := localinbox.Ingest(context.Background(), db, path); err != nil {
		t.Fatalf("Ingest() error: %v", err)
	}
}

func testLogger() *log.Logger {
	return log.New(os.Stderr, "test", 0)
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

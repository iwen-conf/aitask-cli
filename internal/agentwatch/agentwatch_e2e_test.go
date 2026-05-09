package agentwatch

import (
	"context"
	"database/sql"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	localinbox "github.com/iwen-conf/aitask-cli/internal/inbox"
	localstate "github.com/iwen-conf/aitask-cli/internal/state"
	localworker "github.com/iwen-conf/aitask-cli/internal/worker"
)

func TestEndToEnd_MentionToHandled(t *testing.T) {
	db := newE2EDB(t)
	ndjson := writeE2EEvents(t, `{"kind":"mention","ts":"2026-05-08T00:00:00Z","project":"prj_1","eventId":"evt_1","messageId":"thr_1","from":{"agentType":"codex"},"content":"@claude-code please ack","mentions":["claude-code"]}
{"kind":"mention","ts":"2026-05-08T00:01:00Z","project":"prj_1","eventId":"evt_2","messageId":"thr_1","from":{"agentType":"gemini"},"content":"@claude-code second","mentions":["claude-code"],"details":{"wake":false}}
`)
	if _, err := localworker.RunOnce(context.Background(), localworker.Options{StateDB: db, NDJSONPath: ndjson, Logger: e2eLogger()}); err != nil {
		t.Fatalf("worker RunOnce() error: %v", err)
	}
	runner := shellRunner{path: writeHandler(t, `#!/usr/bin/env sh
echo "ACK $AITASK_EVENT_ID"
exit 0
`)}
	stats, err := RunOnce(context.Background(), Options{StateDB: db, Agent: "claude-code", Runner: runner, Logger: e2eLogger()})
	if err != nil {
		t.Fatalf("agentwatch RunOnce() error: %v", err)
	}
	if stats.Picked != 2 || stats.Acked != 2 || stats.Done != 2 || stats.Failed != 0 {
		t.Fatalf("stats = %#v", stats)
	}
	assertInboxStatus(t, db, "evt_1", "claude-code", "handled", 0, true, "")
	assertInboxStatus(t, db, "evt_2", "claude-code", "handled", 0, true, "")
	assertRawJSONPresent(t, db, "evt_1", "@claude-code please ack")
}

func TestEndToEnd_HandlerFailureMarksFailed(t *testing.T) {
	db := newE2EDB(t)
	ndjson := writeE2EEvents(t, `{"kind":"mention","ts":"2026-05-08T00:00:00Z","project":"prj_1","eventId":"evt_1","messageId":"thr_1","from":{"agentType":"gemini"},"content":"@claude-code fail","mentions":["claude-code"]}
`)
	if _, err := localworker.RunOnce(context.Background(), localworker.Options{StateDB: db, NDJSONPath: ndjson, Logger: e2eLogger()}); err != nil {
		t.Fatalf("worker RunOnce() error: %v", err)
	}
	runner := shellRunner{path: writeHandler(t, `#!/usr/bin/env sh
echo "boom" >&2
exit 1
`)}
	stats, err := RunOnce(context.Background(), Options{StateDB: db, Agent: "claude-code", Runner: runner, Logger: e2eLogger(), MaxRetries: 5})
	if err != nil {
		t.Fatalf("agentwatch RunOnce() error: %v", err)
	}
	if stats.Picked != 1 || stats.Acked != 1 || stats.Failed != 1 || stats.Done != 0 {
		t.Fatalf("stats = %#v", stats)
	}
	assertInboxStatus(t, db, "evt_1", "claude-code", "failed", 1, false, "boom")
	assertLastErrorMaxBytes(t, db, "evt_1", "claude-code", 256)
}

func TestEndToEnd_SkipsSelfSent(t *testing.T) {
	db := newE2EDB(t)
	ndjson := writeE2EEvents(t, `{"kind":"mention","ts":"2026-05-08T00:00:00Z","project":"prj_1","eventId":"evt_self","messageId":"thr_1","from":{"agentType":"claude-code"},"content":"@codex self sent","mentions":["codex"]}
`)
	if _, err := localworker.RunOnce(context.Background(), localworker.Options{StateDB: db, NDJSONPath: ndjson, Logger: e2eLogger()}); err != nil {
		t.Fatalf("worker RunOnce() error: %v", err)
	}
	runner := shellRunner{path: writeHandler(t, `#!/usr/bin/env sh
echo should-not-run
exit 0
`)}
	stats, err := RunOnce(context.Background(), Options{StateDB: db, Agent: "claude-code", Runner: runner, Logger: e2eLogger()})
	if err != nil {
		t.Fatalf("agentwatch RunOnce() error: %v", err)
	}
	if stats.Picked != 0 || stats.Done != 0 || stats.Failed != 0 {
		t.Fatalf("stats = %#v, want no pickup", stats)
	}
	rows, err := localinbox.ListAgentInbox(context.Background(), db, "claude-code", localinbox.ListOpts{AllStatuses: true})
	if err != nil {
		t.Fatalf("ListAgentInbox() error: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("claude-code inbox = %#v, want empty", rows)
	}
}

type shellRunner struct {
	path string
}

func (r shellRunner) Run(ctx context.Context, prompt string) (RunResult, error) {
	cmd := exec.CommandContext(ctx, r.path)
	cmd.Stdin = strings.NewReader(prompt)
	cmd.Env = append(os.Environ(), "AITASK_EVENT_ID="+extractEventID(prompt))
	stdout, err := cmd.Output()
	if exitErr, ok := err.(*exec.ExitError); ok {
		return RunResult{Stdout: string(stdout), Stderr: string(exitErr.Stderr), ExitCode: exitErr.ExitCode()}, nil
	}
	if err != nil {
		return RunResult{}, err
	}
	return RunResult{Stdout: string(stdout), ExitCode: 0}, nil
}

func extractEventID(prompt string) string {
	for _, line := range strings.Split(prompt, "\n") {
		if strings.HasPrefix(line, "Event: `") && strings.HasSuffix(line, "`") {
			return strings.TrimSuffix(strings.TrimPrefix(line, "Event: `"), "`")
		}
		if strings.HasPrefix(line, "- ID: ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "- ID: "))
		}
	}
	return ""
}

func newE2EDB(t *testing.T) *sql.DB {
	t.Helper()
	db, closeDB, err := localstate.OpenPath(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("OpenPath() error: %v", err)
	}
	t.Cleanup(func() { _ = closeDB() })
	if err := localstate.Migrate(context.Background(), db); err != nil {
		t.Fatalf("Migrate() error: %v", err)
	}
	return db
}

func writeE2EEvents(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "events.ndjson")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write events: %v", err)
	}
	return path
}

func writeHandler(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "handler.sh")
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write handler: %v", err)
	}
	return path
}

func assertInboxStatus(t *testing.T, db *sql.DB, eventID, agent, status string, retryCount int, handledAt bool, lastErrorContains string) {
	t.Helper()
	var gotStatus, gotHandledAt, gotLastError string
	var gotRetries int
	err := db.QueryRowContext(context.Background(), `SELECT status, COALESCE(handled_at, ''), retry_count, COALESCE(last_error, '')
FROM agent_inbox WHERE event_id=? AND agent=?`, eventID, agent).Scan(&gotStatus, &gotHandledAt, &gotRetries, &gotLastError)
	if err != nil {
		t.Fatalf("query inbox status: %v", err)
	}
	if gotStatus != status || gotRetries != retryCount {
		t.Fatalf("event %s status=%s retries=%d, want %s/%d", eventID, gotStatus, gotRetries, status, retryCount)
	}
	if handledAt && gotHandledAt == "" {
		t.Fatalf("event %s handled_at empty", eventID)
	}
	if lastErrorContains != "" && !strings.Contains(gotLastError, lastErrorContains) {
		t.Fatalf("event %s last_error=%q, want contains %q", eventID, gotLastError, lastErrorContains)
	}
}

func assertRawJSONPresent(t *testing.T, db *sql.DB, eventID, contains string) {
	t.Helper()
	var raw string
	if err := db.QueryRowContext(context.Background(), `SELECT raw_json FROM events WHERE id=?`, eventID).Scan(&raw); err != nil {
		t.Fatalf("query raw_json: %v", err)
	}
	if !strings.Contains(raw, contains) {
		t.Fatalf("raw_json = %q, want contains %q", raw, contains)
	}
}

func assertLastErrorMaxBytes(t *testing.T, db *sql.DB, eventID, agent string, max int) {
	t.Helper()
	var lastError string
	if err := db.QueryRowContext(context.Background(), `SELECT COALESCE(last_error, '') FROM agent_inbox WHERE event_id=? AND agent=?`, eventID, agent).Scan(&lastError); err != nil {
		t.Fatalf("query last_error: %v", err)
	}
	if len([]byte(lastError)) > max {
		t.Fatalf("last_error length = %d, want <= %d", len([]byte(lastError)), max)
	}
}

func e2eLogger() *log.Logger {
	return log.New(os.Stderr, "test-e2e", 0)
}

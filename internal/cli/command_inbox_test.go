package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	localinbox "github.com/iwen-conf/aitask-cli/internal/inbox"
	localstate "github.com/iwen-conf/aitask-cli/internal/state"
)

func TestInboxCommandFallbackFromNDJSON(t *testing.T) {
	home := seedInboxHome(t, `{"kind":"mention","ts":"2026-05-07T12:00:00Z","project":"prj_1","eventId":"evt_1","messageId":"msg_1","from":{"agentType":"claude-code"},"content":"@codex handle","mentions":["codex"]}
{"kind":"mention","ts":"2026-05-07T12:01:00Z","project":"prj_1","eventId":"evt_2","messageId":"msg_2","from":{"agentType":"codex"},"content":"@codex self","mentions":["codex"]}
`)
	t.Setenv("HOME", home)
	t.Setenv(localstate.EnvStateDB, filepath.Join(home, ".aitask", "missing.db"))
	t.Setenv(envProfileName, "codex")

	stdout, err := runInboxTestCommand("inbox", "--agent", "codex", "--format", "json")
	if err != nil {
		t.Fatalf("inbox command error: %v", err)
	}
	var rows []localinbox.InboxRow
	if err := json.Unmarshal([]byte(stdout), &rows); err != nil {
		t.Fatalf("decode stdout: %v\n%s", err, stdout)
	}
	if len(rows) != 1 || rows[0].ID != "evt_1" {
		t.Fatalf("rows = %#v, want only evt_1", rows)
	}
}

func TestInboxGlobalFallback(t *testing.T) {
	home := seedInboxHome(t, `{"kind":"broadcast","ts":"2026-05-07T12:00:00Z","project":"prj_1","eventId":"evt_global","content":"hello all"}
{"kind":"mention","ts":"2026-05-07T12:01:00Z","project":"prj_1","eventId":"evt_direct","from":{"agentType":"claude-code"},"content":"@codex handle","mentions":["codex"]}
`)
	t.Setenv("HOME", home)
	t.Setenv(localstate.EnvStateDB, filepath.Join(home, ".aitask", "missing.db"))
	withProjectDir(t, "prj_1")

	stdout, err := runInboxTestCommand("--format", "json", "inbox", "--global")
	if err != nil {
		t.Fatalf("inbox --global error: %v", err)
	}
	var rows []localinbox.InboxRow
	if err := json.Unmarshal([]byte(stdout), &rows); err != nil {
		t.Fatalf("decode stdout: %v\n%s", err, stdout)
	}
	if len(rows) != 1 || rows[0].ID != "evt_global" {
		t.Fatalf("global rows = %#v, want evt_global", rows)
	}
}

func TestLatestLimitSortsDescending(t *testing.T) {
	home := seedInboxHome(t, `{"kind":"broadcast","ts":"2026-05-07T12:00:00Z","project":"prj_1","eventId":"evt_1","content":"one"}
{"kind":"broadcast","ts":"2026-05-07T12:02:00Z","project":"prj_1","eventId":"evt_3","content":"three"}
{"kind":"broadcast","ts":"2026-05-07T12:01:00Z","project":"prj_1","eventId":"evt_2","content":"two"}
`)
	t.Setenv("HOME", home)
	t.Setenv(localstate.EnvStateDB, filepath.Join(home, ".aitask", "missing.db"))

	stdout, err := runInboxTestCommand("--format", "json", "latest", "--limit", "2")
	if err != nil {
		t.Fatalf("latest command error: %v", err)
	}
	var rows []localinbox.EventRow
	if err := json.Unmarshal([]byte(stdout), &rows); err != nil {
		t.Fatalf("decode stdout: %v\n%s", err, stdout)
	}
	if len(rows) != 2 || rows[0].ID != "evt_3" || rows[1].ID != "evt_2" {
		t.Fatalf("latest rows = %#v, want evt_3, evt_2", rows)
	}
}

func TestAckTwiceViaCLI(t *testing.T) {
	home := seedInboxHome(t, `{"kind":"mention","ts":"2026-05-07T12:00:00Z","project":"prj_1","eventId":"evt_1","from":{"agentType":"claude-code"},"content":"@codex handle","mentions":["codex"]}
`)
	dbPath := filepath.Join(home, ".aitask", "state.db")
	t.Setenv("HOME", home)
	t.Setenv(localstate.EnvStateDB, dbPath)
	db, closeDB, err := localstate.OpenPath(t.Context(), dbPath)
	if err != nil {
		t.Fatalf("OpenPath() error: %v", err)
	}
	if err := localstate.Migrate(t.Context(), db); err != nil {
		t.Fatalf("Migrate() error: %v", err)
	}
	if err := localinbox.Ingest(t.Context(), db, filepath.Join(home, ".aitask", "events.ndjson")); err != nil {
		t.Fatalf("Ingest() error: %v", err)
	}
	_ = closeDB()

	if _, err := runInboxTestCommand("--format", "json", "ack", "evt_1", "--agent", "codex"); err != nil {
		t.Fatalf("first ack error: %v", err)
	}
	if _, err := runInboxTestCommand("--format", "json", "ack", "evt_1", "--agent", "codex"); err == nil || !strings.Contains(err.Error(), localinbox.ErrNotApplicable.Error()) {
		t.Fatalf("second ack error = %v, want ErrNotApplicable", err)
	}
}

func TestThreadCommand(t *testing.T) {
	home := seedInboxHome(t, `{"kind":"mention","ts":"2026-05-07T12:00:00Z","project":"prj_1","eventId":"evt_1","messageId":"thr_1","from":{"agentType":"claude-code"},"content":"@codex handle","mentions":["codex"]}
{"kind":"broadcast","ts":"2026-05-07T12:01:00Z","project":"prj_1","eventId":"evt_2","content":"other"}
`)
	t.Setenv("HOME", home)
	t.Setenv(localstate.EnvStateDB, filepath.Join(home, ".aitask", "missing.db"))

	stdout, err := runInboxTestCommand("--format", "json", "thread", "thr_1")
	if err != nil {
		t.Fatalf("thread command error: %v", err)
	}
	var rows []localinbox.EventRow
	if err := json.Unmarshal([]byte(stdout), &rows); err != nil {
		t.Fatalf("decode stdout: %v\n%s", err, stdout)
	}
	if len(rows) != 1 || rows[0].ID != "evt_1" {
		t.Fatalf("thread rows = %#v, want evt_1", rows)
	}
}

func runInboxTestCommand(args ...string) (string, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp("test")
	app.Stdout = &stdout
	app.Stderr = &stderr
	app.Stdin = strings.NewReader("")
	err := app.Execute(args)
	if err != nil && stderr.Len() > 0 && !errors.Is(err, localinbox.ErrNotApplicable) {
		return stdout.String(), errors.New(stderr.String() + err.Error())
	}
	return stdout.String(), err
}

func seedInboxHome(t *testing.T, ndjson string) string {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, ".aitask")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir home .aitask: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "events.ndjson"), []byte(ndjson), 0o644); err != nil {
		t.Fatalf("write events.ndjson: %v", err)
	}
	return home
}

func withProjectDir(t *testing.T, projectID string) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".aitask"), 0o755); err != nil {
		t.Fatalf("mkdir .aitask: %v", err)
	}
	doc := "# AI Task Project\nproject_id: " + projectID + "\nproject_name: Demo\nopenviking_root: viking://aitask/projects/" + projectID + "\nroom_enabled: true\n"
	if err := os.WriteFile(filepath.Join(root, ".aitask", "project.md"), []byte(doc), 0o644); err != nil {
		t.Fatalf("write project doc: %v", err)
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("Chdir() error: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })
}

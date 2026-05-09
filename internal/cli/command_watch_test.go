package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	localstate "github.com/iwen-conf/aitask-cli/internal/state"
)

func TestWatchCommandOnceDryRun(t *testing.T) {
	home := seedWatchHome(t, `{"kind":"mention","ts":"2026-05-07T12:00:00Z","project":"prj_1","eventId":"evt_1","messageId":"thr_1","from":{"agentType":"claude-code"},"content":"@codex handle","mentions":["codex"]}
`)
	t.Setenv("HOME", home)
	t.Setenv(localstate.EnvStateDB, filepath.Join(home, ".aitask", "state.db"))
	if _, err := runWatchTestCommand("worker", "--once", "--memory", "none", "--quiet"); err != nil {
		t.Fatalf("seed worker error: %v", err)
	}
	stdout, err := runWatchTestCommand("watch", "--agent", "codex", "--once", "--dry-run", "--quiet")
	if err != nil {
		t.Fatalf("watch command error: %v", err)
	}
	if !strings.Contains(stdout, "You are codex") || !strings.Contains(stdout, "@codex handle") {
		t.Fatalf("stdout missing prompt: %s", stdout)
	}
}

func runWatchTestCommand(args ...string) (string, error) {
	var stdout, stderr bytes.Buffer
	app := NewApp("test")
	app.Stdout = &stdout
	app.Stderr = &stderr
	app.Stdin = strings.NewReader("")
	err := app.Execute(args)
	if err != nil && stderr.Len() > 0 {
		return stdout.String(), err
	}
	return stdout.String(), err
}

func seedWatchHome(t *testing.T, ndjson string) string {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, ".aitask")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir .aitask: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "events.ndjson"), []byte(ndjson), 0o600); err != nil {
		t.Fatalf("write events: %v", err)
	}
	return home
}

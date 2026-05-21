package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkerCommandOnce(t *testing.T) {
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
	err := app.Execute([]string{"--format", "brief", "worker", "--once", "--quiet"})
	if err != nil {
		t.Fatalf("Execute() error: %v; stderr=%s", err, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) == "" {
		t.Fatalf("worker output is empty")
	}
}

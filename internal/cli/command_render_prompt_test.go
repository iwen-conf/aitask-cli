package cli

import (
	"strings"
	"testing"

	localstate "github.com/iwen-conf/aitask-cli/internal/state"
)

func TestRenderPromptCommandNoRecall(t *testing.T) {
	home := seedWatchHome(t, `{"kind":"mention","ts":"2026-05-07T12:00:00Z","project":"prj_1","eventId":"evt_1","messageId":"thr_1","from":{"agentType":"claude-code"},"content":"@codex handle","mentions":["codex"]}
`)
	t.Setenv("HOME", home)
	t.Setenv(localstate.EnvStateDB, home+"/.aitask/state.db")
	if _, err := runWatchTestCommand("worker", "--once", "--memory", "none", "--quiet"); err != nil {
		t.Fatalf("seed worker error: %v", err)
	}
	stdout, err := runWatchTestCommand("render-prompt", "--event", "evt_1", "--agent", "codex", "--no-recall")
	if err != nil {
		t.Fatalf("render-prompt command error: %v", err)
	}
	if !strings.Contains(stdout, "You are codex") || !strings.Contains(stdout, "Relevant Context:\n(none)") {
		t.Fatalf("stdout missing rendered prompt: %s", stdout)
	}
}

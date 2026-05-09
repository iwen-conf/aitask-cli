package agentwatch

import (
	"strings"
	"testing"
)

func TestRenderPromptTemplate(t *testing.T) {
	prompt := RenderPrompt("evt_1", "codex", "memory context", PromptEvent{
		ID:        "evt_1",
		Kind:      "mention",
		From:      "claude-code",
		Project:   "prj_1",
		ThreadID:  "thr_1",
		CreatedAt: "2026-05-07T12:00:00Z",
		Body:      "@codex handle this",
	})
	for _, want := range []string{
		"You are codex in an AITask multi-agent collaboration system.",
		"- ID: evt_1",
		"- From: claude-code",
		"- To: codex",
		"@codex handle this",
		"memory context",
		"Do not process events sent by yourself.",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

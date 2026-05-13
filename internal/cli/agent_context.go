package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// AgentKind identifies a supported AI agent runtime whose project-level
// context file we manage.
type AgentKind string

const (
	AgentClaude AgentKind = "claude"
	AgentCodex  AgentKind = "codex"
	AgentGemini AgentKind = "gemini"
)

// DefaultAgentKinds is the order applied when --agents is empty.
var DefaultAgentKinds = []AgentKind{AgentClaude, AgentCodex, AgentGemini}

const (
	agentContextBeginMarker = "<!-- BEGIN aitask:context -->"
	agentContextEndMarker   = "<!-- END aitask:context -->"
)

func agentFileName(kind AgentKind) string {
	switch kind {
	case AgentClaude:
		return "CLAUDE.md"
	case AgentCodex:
		return "AGENTS.md"
	case AgentGemini:
		return "GEMINI.md"
	}
	return ""
}

// ParseAgentKinds turns a comma-separated --agents flag into a deduped,
// order-preserving list. An empty input yields DefaultAgentKinds.
func ParseAgentKinds(raw string) ([]AgentKind, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return append([]AgentKind(nil), DefaultAgentKinds...), nil
	}
	parts := strings.Split(raw, ",")
	kinds := make([]AgentKind, 0, len(parts))
	seen := map[AgentKind]bool{}
	for _, p := range parts {
		p = strings.TrimSpace(strings.ToLower(p))
		if p == "" {
			continue
		}
		kind := AgentKind(p)
		switch kind {
		case AgentClaude, AgentCodex, AgentGemini:
		default:
			return nil, fmt.Errorf("unknown agent %q (allowed: claude, codex, gemini)", p)
		}
		if seen[kind] {
			continue
		}
		seen[kind] = true
		kinds = append(kinds, kind)
	}
	if len(kinds) == 0 {
		return append([]AgentKind(nil), DefaultAgentKinds...), nil
	}
	return kinds, nil
}

// WriteAgentContextFiles writes (or refreshes) the aitask context block in
// each requested agent's project-root markdown file. Returns the paths that
// were created or modified.
func WriteAgentContextFiles(rootDir string, kinds []AgentKind, values ProjectDocValues) ([]string, error) {
	root := strings.TrimSpace(rootDir)
	if root == "" {
		return nil, fmt.Errorf("rootDir cannot be empty")
	}
	if len(kinds) == 0 {
		kinds = append([]AgentKind(nil), DefaultAgentKinds...)
	}
	values = normalizeProjectDocValues(values)
	if values.ProjectID == "" {
		return nil, fmt.Errorf("project_id cannot be empty")
	}
	block := renderAgentContextBlock(values)

	changed := []string{}
	for _, kind := range kinds {
		name := agentFileName(kind)
		if name == "" {
			continue
		}
		path := filepath.Join(root, name)
		updated, err := upsertAgentContextBlock(path, block)
		if err != nil {
			return nil, err
		}
		if updated {
			changed = append(changed, path)
		}
	}
	return changed, nil
}

// renderAgentContextBlock produces the marker-delimited aitask block injected
// into AGENTS.md / CLAUDE.md / GEMINI.md.
func renderAgentContextBlock(values ProjectDocValues) string {
	values = normalizeProjectDocValues(values)
	name := values.ProjectName
	if name == "" {
		name = values.ProjectID
	}
	var b strings.Builder
	b.WriteString(agentContextBeginMarker)
	b.WriteString("\n## AITask Integration\n\n")
	b.WriteString("This project is managed by **AITask** (`aitask` CLI). Follow these rules at session start.\n\n")
	b.WriteString(fmt.Sprintf("- Project ID: `%s`\n", values.ProjectID))
	b.WriteString(fmt.Sprintf("- Project Name: `%s`\n", name))
	if values.OpenVikingRoot != "" {
		b.WriteString(fmt.Sprintf("- OpenViking root: `%s`\n", values.OpenVikingRoot))
	}
	b.WriteString("\n### Required startup sequence\n\n")
	b.WriteString("```bash\n")
	b.WriteString("aitask whoami         # confirm agent identity\n")
	b.WriteString("aitask bootstrap      # load project context\n")
	b.WriteString("aitask task current   # check delegated work\n")
	b.WriteString("```\n\n")
	b.WriteString("If `aitask task current` reports no active task, run `aitask task inbox`.\n\n")
	b.WriteString("### Live event stream\n\n")
	b.WriteString("Mention and task-delegation events stream into `~/.aitask/events.ndjson`. ")
	b.WriteString("Tail the file with your runtime's background monitor (Claude `Monitor`, `tail -F`, etc.).\n\n")
	b.WriteString("The `aitask` CLI auto-launches the `aitask-watch` tmux daemon on first invocation inside this project; no manual setup is required.\n\n")
	b.WriteString("### Forbidden\n\n")
	b.WriteString("- Do not edit `.aitask/state/*.pb` manually.\n")
	b.WriteString("- Do not run delegated tasks outside the CLI lifecycle.\n")
	b.WriteString("- Do not rely on chat history; rebuild context via `aitask bootstrap`.\n\n")
	b.WriteString(agentContextEndMarker)
	b.WriteString("\n")
	return b.String()
}

// upsertAgentContextBlock writes or replaces the marker-delimited aitask
// block at path. Returns true when the file was created or its bytes
// changed. The function preserves user content outside the markers.
func upsertAgentContextBlock(path, block string) (bool, error) {
	existing, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return false, err
		}
		if err := writeTextFile(path, block); err != nil {
			return false, err
		}
		return true, nil
	}

	body := string(existing)
	begin := strings.Index(body, agentContextBeginMarker)
	end := strings.Index(body, agentContextEndMarker)

	if begin < 0 || end < 0 || end < begin {
		// No (or corrupt) marker pair: append a fresh block, keeping
		// existing user content intact.
		trimmed := strings.TrimRight(body, "\n")
		next := trimmed + "\n\n" + block
		if next == body {
			return false, nil
		}
		return true, os.WriteFile(path, []byte(next), 0o644)
	}

	endStop := end + len(agentContextEndMarker)
	// Swallow exactly one trailing newline after the end marker so the
	// replacement remains line-anchored.
	if endStop < len(body) && body[endStop] == '\n' {
		endStop++
	}
	next := body[:begin] + block + body[endStop:]
	if next == body {
		return false, nil
	}
	return true, os.WriteFile(path, []byte(next), 0o644)
}

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseAgentKinds_Default(t *testing.T) {
	got, err := ParseAgentKinds("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 3 || got[0] != AgentClaude || got[1] != AgentCodex || got[2] != AgentGemini {
		t.Fatalf("default order = %v", got)
	}
}

func TestParseAgentKinds_Subset(t *testing.T) {
	got, err := ParseAgentKinds("codex, claude")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 || got[0] != AgentCodex || got[1] != AgentClaude {
		t.Fatalf("subset order = %v", got)
	}
}

func TestParseAgentKinds_Dedup(t *testing.T) {
	got, err := ParseAgentKinds("codex,codex,gemini")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 || got[0] != AgentCodex || got[1] != AgentGemini {
		t.Fatalf("dedup order = %v", got)
	}
}

func TestParseAgentKinds_Invalid(t *testing.T) {
	if _, err := ParseAgentKinds("foo"); err == nil {
		t.Fatalf("expected error for unknown agent")
	}
}

func TestUpsertAgentContextBlock_NewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	block := renderAgentContextBlock(ProjectDocValues{ProjectID: "prj_demo"})

	changed, err := upsertAgentContextBlock(path, block)
	if err != nil {
		t.Fatalf("upsert error: %v", err)
	}
	if !changed {
		t.Fatal("expected file to be created")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "prj_demo") {
		t.Fatalf("file missing project id, got: %s", got)
	}
	if !strings.Contains(string(got), agentContextBeginMarker) {
		t.Fatalf("missing begin marker")
	}
}

func TestUpsertAgentContextBlock_AppendPreservesUserContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")
	original := "# My Project\n\nCustom rules.\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	block := renderAgentContextBlock(ProjectDocValues{ProjectID: "prj_demo"})

	changed, err := upsertAgentContextBlock(path, block)
	if err != nil {
		t.Fatalf("upsert error: %v", err)
	}
	if !changed {
		t.Fatal("expected change=true")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(got)
	if !strings.HasPrefix(body, "# My Project") {
		t.Fatalf("user content not preserved: %s", body)
	}
	if !strings.Contains(body, "Custom rules.") {
		t.Fatalf("user content lost: %s", body)
	}
	if !strings.Contains(body, "prj_demo") {
		t.Fatalf("aitask block missing")
	}
}

func TestUpsertAgentContextBlock_ReplaceInPlace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	oldBlock := renderAgentContextBlock(ProjectDocValues{ProjectID: "prj_old"})
	original := "# Header\n\n" + oldBlock + "\ntrailing user note\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	newBlock := renderAgentContextBlock(ProjectDocValues{ProjectID: "prj_new"})

	changed, err := upsertAgentContextBlock(path, newBlock)
	if err != nil {
		t.Fatalf("upsert error: %v", err)
	}
	if !changed {
		t.Fatal("expected change=true")
	}
	got, _ := os.ReadFile(path)
	body := string(got)
	if strings.Contains(body, "prj_old") {
		t.Fatalf("old project id still present: %s", body)
	}
	if !strings.Contains(body, "prj_new") {
		t.Fatalf("new project id missing: %s", body)
	}
	if !strings.Contains(body, "trailing user note") {
		t.Fatalf("trailing user content lost: %s", body)
	}
	if !strings.HasPrefix(body, "# Header") {
		t.Fatalf("leading user content lost: %s", body)
	}
	// Only one begin/end marker pair must remain.
	if strings.Count(body, agentContextBeginMarker) != 1 {
		t.Fatalf("expected exactly 1 begin marker, body: %s", body)
	}
	if strings.Count(body, agentContextEndMarker) != 1 {
		t.Fatalf("expected exactly 1 end marker, body: %s", body)
	}
}

func TestUpsertAgentContextBlock_NoChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	block := renderAgentContextBlock(ProjectDocValues{ProjectID: "prj_demo"})
	if _, err := upsertAgentContextBlock(path, block); err != nil {
		t.Fatal(err)
	}
	infoBefore, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := upsertAgentContextBlock(path, block)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("expected change=false on identical content")
	}
	infoAfter, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if infoBefore.ModTime() != infoAfter.ModTime() {
		t.Fatalf("file was rewritten despite no content change")
	}
}

func TestWriteAgentContextFiles_Subset(t *testing.T) {
	dir := t.TempDir()
	written, err := WriteAgentContextFiles(dir, []AgentKind{AgentCodex}, ProjectDocValues{ProjectID: "prj_demo"})
	if err != nil {
		t.Fatalf("WriteAgentContextFiles: %v", err)
	}
	if len(written) != 1 {
		t.Fatalf("expected 1 file, got %v", written)
	}
	if filepath.Base(written[0]) != "AGENTS.md" {
		t.Fatalf("expected AGENTS.md, got %s", written[0])
	}
	for _, name := range []string{"CLAUDE.md", "GEMINI.md"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("%s should not exist", name)
		}
	}
}

func TestWriteAgentContextFiles_DefaultAllThree(t *testing.T) {
	dir := t.TempDir()
	written, err := WriteAgentContextFiles(dir, nil, ProjectDocValues{ProjectID: "prj_demo"})
	if err != nil {
		t.Fatalf("WriteAgentContextFiles: %v", err)
	}
	if len(written) != 3 {
		t.Fatalf("expected 3 files, got %v", written)
	}
}

func TestWriteAgentContextFiles_MissingProjectID(t *testing.T) {
	dir := t.TempDir()
	if _, err := WriteAgentContextFiles(dir, DefaultAgentKinds, ProjectDocValues{}); err == nil {
		t.Fatal("expected error for empty project id")
	}
}

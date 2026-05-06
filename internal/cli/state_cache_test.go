package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAndReadStateJSON(t *testing.T) {
	root := t.TempDir()
	aiDir := filepath.Join(root, ".aitask")
	if _, err := EnsureAITaskLayout(root); err != nil {
		t.Fatalf("EnsureAITaskLayout error: %v", err)
	}
	payload := map[string]any{"state": "normal", "used": 123, "warnings": []string{"near_limit"}}
	if err := writeStateJSON(aiDir, stateContextUsagePB, payload); err != nil {
		t.Fatalf("writeStateJSON error: %v", err)
	}
	got, err := readStateJSON(aiDir, stateContextUsagePB)
	if err != nil {
		t.Fatalf("readStateJSON error: %v", err)
	}
	if mapString(got, "state") != "normal" {
		t.Fatalf("state mismatch")
	}
	if mapInt(got, "used") != 123 {
		t.Fatalf("used mismatch")
	}
	if warnings := asSlice(got["warnings"]); len(warnings) != 1 {
		t.Fatalf("warnings length = %d, want 1", len(warnings))
	}
}

func TestReadStateJSONCorruptedRemovesFile(t *testing.T) {
	root := t.TempDir()
	aiDir := filepath.Join(root, ".aitask")
	if _, err := EnsureAITaskLayout(root); err != nil {
		t.Fatalf("EnsureAITaskLayout error: %v", err)
	}
	target := filepath.Join(aiDir, "state", stateContextUsagePB)
	if err := os.WriteFile(target, []byte("broken"), 0o644); err != nil {
		t.Fatalf("write broken file error: %v", err)
	}
	if _, err := readStateJSON(aiDir, stateContextUsagePB); err == nil {
		t.Fatalf("expected error for corrupted cache")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("corrupted cache should be removed")
	}
}

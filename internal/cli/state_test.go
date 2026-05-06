package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadProjectConfig(t *testing.T) {
	temp := t.TempDir()
	projectDir := filepath.Join(temp, "repo")
	if err := os.MkdirAll(filepath.Join(projectDir, ".aitask"), 0o755); err != nil {
		t.Fatal(err)
	}
	content := `# AI Task Project
project_id: prj_test
project_name: Demo
openviking_root: viking://aitask/projects/prj_test
room_enabled: true
`
	if err := os.WriteFile(filepath.Join(projectDir, ".aitask", "project.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	nested := filepath.Join(projectDir, "a", "b", "c")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadProjectConfig(nested)
	if err != nil {
		t.Fatalf("LoadProjectConfig failed: %v", err)
	}
	if cfg.ProjectID != "prj_test" {
		t.Fatalf("project id = %q", cfg.ProjectID)
	}
	if cfg.ProjectName != "Demo" {
		t.Fatalf("project name = %q", cfg.ProjectName)
	}
	if cfg.OpenVikingRoot == "" {
		t.Fatalf("openviking root should not be empty")
	}
	if !cfg.RoomEnabled {
		t.Fatalf("room should be enabled")
	}
}

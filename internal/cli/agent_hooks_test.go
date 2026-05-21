package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func seedInstalled(t *testing.T, home string, kind AgentKind) {
	t.Helper()
	layout, ok := hookLayouts[kind]
	if !ok {
		t.Fatalf("unknown kind: %s", kind)
	}
	base := filepath.Join(home, layout.homeSubdir)
	for _, name := range layout.scripts {
		writeFile(t, filepath.Join(base, "hooks", name), "#!/usr/bin/env bash\n")
	}
	settings := map[string]any{
		"hooks": map[string]any{
			"SessionStart": []any{map[string]any{"hooks": []any{
				map[string]any{"command": "$HOME/" + layout.homeSubdir + "/hooks/aitask-session-start.sh"},
			}}},
		},
	}
	data, _ := json.Marshal(settings)
	writeFile(t, filepath.Join(base, layout.settingsFile), string(data))
}

func writeFakeInstall(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "scripts", "aitask-hooks", "install.sh")
	body := `#!/usr/bin/env bash
set -euo pipefail
agent="${2:-all}"
case "$agent" in
  claude) sub=".claude"; settings_name=settings.json ;;
  codex)  sub=".codex";  settings_name=hooks.json ;;
  gemini) sub=".gemini"; settings_name=settings.json ;;
  *) echo "unsupported $agent" >&2; exit 1 ;;
esac
mkdir -p "$HOME/$sub/hooks"
touch "$HOME/$sub/hooks/aitask-session-start.sh"
touch "$HOME/$sub/hooks/aitask-prompt-submit.sh"
touch "$HOME/$sub/hooks/aitask-before-agent.sh"
printf '%s' '{"hooks":{"SessionStart":[{"hooks":[{"command":"aitask-stub"}]}]}}' > "$HOME/$sub/$settings_name"
`
	writeFile(t, path, body)
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	return path
}

func TestEnsureAgentHooks_DetectsInstalled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, k := range DefaultAgentKinds {
		seedInstalled(t, home, k)
	}
	result, err := EnsureAgentHooks("", DefaultAgentKinds)
	if err != nil {
		t.Fatalf("EnsureAgentHooks: %v", err)
	}
	if len(result.Installed) != 0 || len(result.Failed) != 0 {
		t.Fatalf("expected all already-installed, got %+v", result)
	}
	if len(result.AlreadyInstalled) != len(DefaultAgentKinds) {
		t.Fatalf("expected %d already-installed, got %d", len(DefaultAgentKinds), len(result.AlreadyInstalled))
	}
}

func TestEnsureAgentHooks_InstallScriptNotFound(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("AITASK_HOOKS_INSTALL_SCRIPT", "")
	result, err := EnsureAgentHooks(t.TempDir(), DefaultAgentKinds)
	if err != nil {
		t.Fatalf("EnsureAgentHooks: %v", err)
	}
	if len(result.Failed) != len(DefaultAgentKinds) {
		t.Fatalf("expected all failed, got %+v", result)
	}
	if result.InstallScript != "" {
		t.Fatalf("expected empty InstallScript, got %q", result.InstallScript)
	}
	if len(result.Notes) == 0 || !strings.Contains(result.Notes[0], "install.sh not found") {
		t.Fatalf("expected install.sh-not-found note, got %v", result.Notes)
	}
}

func TestEnsureAgentHooks_ForksInstallScript(t *testing.T) {
	if _, err := os.Stat("/bin/bash"); err != nil {
		t.Skip("/bin/bash unavailable")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := t.TempDir()
	script := writeFakeInstall(t, root)
	t.Setenv("AITASK_HOOKS_INSTALL_SCRIPT", script)

	result, err := EnsureAgentHooks(root, []AgentKind{AgentClaude})
	if err != nil {
		t.Fatalf("EnsureAgentHooks: %v", err)
	}
	if len(result.Installed) != 1 || result.Installed[0] != AgentClaude {
		t.Fatalf("expected claude installed, got %+v", result)
	}
	if len(result.Failed) != 0 {
		t.Fatalf("unexpected failures: %+v", result.Failed)
	}
	if !fileExists(filepath.Join(home, ".claude", "hooks", "aitask-session-start.sh")) {
		t.Fatalf("install.sh did not seed claude session-start")
	}
}

func TestEnsureAgentHooks_Subset(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := t.TempDir()
	t.Setenv("AITASK_HOOKS_INSTALL_SCRIPT", writeFakeInstall(t, root))

	result, err := EnsureAgentHooks(root, []AgentKind{AgentGemini})
	if err != nil {
		t.Fatalf("EnsureAgentHooks: %v", err)
	}
	if len(result.Installed) != 1 || result.Installed[0] != AgentGemini {
		t.Fatalf("expected only gemini installed, got %+v", result)
	}
	for _, kind := range []AgentKind{AgentClaude, AgentCodex} {
		layout := hookLayouts[kind]
		if fileExists(filepath.Join(home, layout.homeSubdir, "hooks", "aitask-session-start.sh")) {
			t.Fatalf("kind %s should not have been touched", kind)
		}
	}
}

func TestFindInstallScript_PrefersEnv(t *testing.T) {
	scriptDir := t.TempDir()
	scriptPath := filepath.Join(scriptDir, "install.sh")
	writeFile(t, scriptPath, "#!/usr/bin/env bash\n")
	t.Setenv("AITASK_HOOKS_INSTALL_SCRIPT", scriptPath)
	got := findInstallScript(t.TempDir())
	if got != scriptPath {
		t.Fatalf("expected env override %q, got %q", scriptPath, got)
	}
}

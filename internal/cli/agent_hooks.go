package cli

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// HookEnsureResult is the outcome of EnsureAgentHooks for one init invocation.
type HookEnsureResult struct {
	AlreadyInstalled []AgentKind
	Installed        []AgentKind
	Failed           []AgentKind
	InstallScript    string
	Notes            []string
	Stdout, Stderr   string
}

// hookLayout describes the files install.sh produces for one CLI.
type hookLayout struct {
	homeSubdir   string
	scripts      []string
	settingsFile string
}

var hookLayouts = map[AgentKind]hookLayout{
	AgentClaude: {
		homeSubdir:   ".claude",
		scripts:      []string{"aitask-session-start.sh", "aitask-prompt-submit.sh"},
		settingsFile: "settings.json",
	},
	AgentCodex: {
		homeSubdir:   ".codex",
		scripts:      []string{"aitask-session-start.sh", "aitask-prompt-submit.sh"},
		settingsFile: "hooks.json",
	},
	AgentGemini: {
		homeSubdir:   ".gemini",
		scripts:      []string{"aitask-session-start.sh", "aitask-before-agent.sh"},
		settingsFile: "settings.json",
	},
}

// EnsureAgentHooks checks whether SessionStart + per-turn hooks for each
// requested CLI are wired into the user's home, and forks the project's
// install.sh to fill in any gaps. Missing install.sh, missing jq, or fork
// failures are reported via the result rather than as Go errors so that
// `aitask init` keeps succeeding.
func EnsureAgentHooks(rootDir string, kinds []AgentKind) (HookEnsureResult, error) {
	if len(kinds) == 0 {
		kinds = append([]AgentKind(nil), DefaultAgentKinds...)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return HookEnsureResult{}, err
	}
	result := HookEnsureResult{}

	pending := make([]AgentKind, 0, len(kinds))
	for _, kind := range kinds {
		if _, ok := hookLayouts[kind]; !ok {
			continue
		}
		if hookKindInstalled(home, kind) {
			result.AlreadyInstalled = append(result.AlreadyInstalled, kind)
			continue
		}
		pending = append(pending, kind)
	}
	if len(pending) == 0 {
		return result, nil
	}

	script := findInstallScript(rootDir)
	result.InstallScript = script
	if script == "" {
		result.Failed = append(result.Failed, pending...)
		result.Notes = append(result.Notes,
			"install.sh not found; set AITASK_HOOKS_INSTALL_SCRIPT or run scripts/aitask-hooks/install.sh manually")
		return result, nil
	}
	if _, err := exec.LookPath("jq"); err != nil {
		result.Failed = append(result.Failed, pending...)
		result.Notes = append(result.Notes, "jq is required by install.sh but was not found on PATH")
		return result, nil
	}

	var stdout, stderr bytes.Buffer
	for _, kind := range pending {
		cmd := exec.Command("bash", script, "--force", string(kind))
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			result.Failed = append(result.Failed, kind)
			result.Notes = append(result.Notes,
				fmt.Sprintf("install.sh failed for %s: %v", kind, err))
			continue
		}
		if hookKindInstalled(home, kind) {
			result.Installed = append(result.Installed, kind)
		} else {
			result.Failed = append(result.Failed, kind)
			result.Notes = append(result.Notes,
				fmt.Sprintf("install.sh ran but %s still appears unwired; check %s", kind, script))
		}
	}
	result.Stdout = strings.TrimSpace(stdout.String())
	result.Stderr = strings.TrimSpace(stderr.String())
	return result, nil
}

func hookKindInstalled(home string, kind AgentKind) bool {
	layout, ok := hookLayouts[kind]
	if !ok {
		return false
	}
	base := filepath.Join(home, layout.homeSubdir)
	for _, name := range layout.scripts {
		path := filepath.Join(base, "hooks", name)
		if _, err := os.Lstat(path); err != nil {
			return false
		}
	}
	data, err := os.ReadFile(filepath.Join(base, layout.settingsFile))
	if err != nil {
		return false
	}
	return bytes.Contains(data, []byte("aitask-"))
}

// findInstallScript locates scripts/aitask-hooks/install.sh, preferring an
// explicit override, then the binary's source tree, then the project root.
func findInstallScript(rootDir string) string {
	if env := strings.TrimSpace(os.Getenv("AITASK_HOOKS_INSTALL_SCRIPT")); env != "" {
		if fileExists(env) {
			return env
		}
	}
	candidates := []string{}
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(exeDir, "..", "scripts", "aitask-hooks", "install.sh"),
			filepath.Join(exeDir, "..", "share", "aitask", "aitask-hooks", "install.sh"),
		)
	}
	if rootDir != "" {
		candidates = append(candidates,
			filepath.Join(rootDir, "scripts", "aitask-hooks", "install.sh"))
		if top := gitOutput(rootDir, "rev-parse", "--show-toplevel"); top != "" {
			candidates = append(candidates,
				filepath.Join(top, "scripts", "aitask-hooks", "install.sh"))
		}
	}
	for _, candidate := range candidates {
		if fileExists(candidate) {
			abs, err := filepath.Abs(candidate)
			if err != nil {
				return candidate
			}
			return abs
		}
	}
	return ""
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

// formatHookEnsureResult renders the result for inclusion in the init prompt.
func formatHookEnsureResult(r HookEnsureResult) string {
	if len(r.AlreadyInstalled)+len(r.Installed)+len(r.Failed) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n## Hook installation\n\n")
	if len(r.Installed) > 0 {
		b.WriteString("- Installed: ")
		b.WriteString(joinKinds(r.Installed))
		b.WriteString("\n")
	}
	if len(r.AlreadyInstalled) > 0 {
		b.WriteString("- Already installed: ")
		b.WriteString(joinKinds(r.AlreadyInstalled))
		b.WriteString("\n")
	}
	if len(r.Failed) > 0 {
		b.WriteString("- Failed: ")
		b.WriteString(joinKinds(r.Failed))
		b.WriteString("\n")
	}
	for _, note := range r.Notes {
		b.WriteString("  - ")
		b.WriteString(note)
		b.WriteString("\n")
	}
	return b.String()
}

func joinKinds(kinds []AgentKind) string {
	parts := make([]string, len(kinds))
	for i, k := range kinds {
		parts[i] = string(k)
	}
	return strings.Join(parts, ", ")
}

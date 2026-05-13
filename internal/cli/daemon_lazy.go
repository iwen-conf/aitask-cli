package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// lazyDaemonSkip lists subcommand names that must NOT trigger lazy
// daemon start (trivial / setup commands).
var lazyDaemonSkip = map[string]bool{
	"version": true,
	"help":    true,
	"init":    true,
	"auth":    true,
	"aitask":  true, // root with no subcommand
}

// maybeStartWatchDaemon launches the aitask-watch tmux session for the
// current project when:
//   - the invoked subcommand is not in lazyDaemonSkip;
//   - the cwd contains an initialized .aitask/project.md;
//   - tmux and the watch binary are on PATH;
//   - no tmux session with the configured name is already running.
//
// The function MUST NOT return errors or block the calling command.
// Daemon spawn failures are silently ignored.
func maybeStartWatchDaemon(cmd *cobra.Command) {
	if cmd == nil {
		return
	}
	if lazyDaemonSkip[cmd.Name()] {
		return
	}
	cwd, err := os.Getwd()
	if err != nil {
		return
	}
	if _, err := os.Stat(filepath.Join(cwd, AITaskDirName, "project.md")); err != nil {
		return
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		return
	}
	watchBin := getenvDefault("AITASK_WATCH_BIN", "aitask-watch")
	if _, err := exec.LookPath(watchBin); err != nil {
		return
	}
	sessionName := getenvDefault("AITASK_WATCH_TMUX", "aitask-watch")
	if exec.Command("tmux", "has-session", "-t", sessionName).Run() == nil {
		return
	}
	args := strings.Fields(getenvDefault("AITASK_WATCH_ARGS", "--notify auto --stdout=false"))
	full := append([]string{"new-session", "-d", "-s", sessionName, watchBin}, args...)
	_ = exec.Command("tmux", full...).Run()
}

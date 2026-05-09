// aitask-watch is the local events.ndjson subscriber daemon.
//
// It subscribes to the AITask backend's actionable event stream (mention /
// task_delegated / global notifications) over WebSocket and appends each
// envelope to ~/.aitask/events.ndjson. Hooks for Claude Code / Codex / Gemini
// auto-launch this binary in a tmux session so their SessionStart prompts can
// reflect the latest project events.
//
// This binary owns event collection only. It does not write to state.db, sync
// to OpenViking, or wake other agents — those are aitask-worker and
// aitask-agent-watch.
package main

import (
	"fmt"
	"os"

	"github.com/iwen-conf/aitask-cli/internal/cli"
)

var version = "dev"

func main() {
	app := cli.NewApp(version)
	err := app.ExecuteSpecialized(
		"aitask-watch",
		"Stream actionable AITask events to ~/.aitask/events.ndjson",
		cli.NewWatchSubcommand,
		os.Args[1:],
	)
	if err != nil {
		cli.WriteCommandError(os.Stderr, err)
		fmt.Fprintln(os.Stderr, "hint: use --help for command usage")
		os.Exit(1)
	}
}

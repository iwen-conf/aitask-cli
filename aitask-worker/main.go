// aitask-worker is the local index + memory sync daemon.
//
// It consumes ~/.aitask/events.ndjson, normalizes events, populates state.db
// (events / agent_inbox / global_feed / cursors / memory_sync / summaries),
// and pushes high-value rows to OpenViking through the AITask backend.
//
// This binary does not subscribe to the WebSocket stream (that is
// aitask-watch) and does not wake other agents (that is aitask-agent-watch).
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
		"aitask-worker",
		"Index local events and sync semantic memory to OpenViking",
		cli.NewWorkerSubcommand,
		os.Args[1:],
	)
	if err != nil {
		cli.WriteCommandError(os.Stderr, err)
		fmt.Fprintln(os.Stderr, "hint: use --help for command usage")
		os.Exit(1)
	}
}

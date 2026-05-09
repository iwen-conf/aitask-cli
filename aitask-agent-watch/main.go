// aitask-agent-watch consumes a specific agent's inbox and optionally drives a
// runner (claude / codex / gemini / custom --exec) to handle the message,
// writing the outcome (done / failed / skipped) back to state.db.
//
// It does not collect raw events (aitask-watch) nor index NDJSON into
// state.db (aitask-worker). It only operates on rows already routed into
// agent_inbox by the worker.
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
		"aitask-agent-watch",
		"Consume a single agent's local inbox and drive its runner",
		cli.NewAgentWatchSubcommand,
		os.Args[1:],
	)
	if err != nil {
		cli.WriteCommandError(os.Stderr, err)
		fmt.Fprintln(os.Stderr, "hint: use --help for command usage")
		os.Exit(1)
	}
}

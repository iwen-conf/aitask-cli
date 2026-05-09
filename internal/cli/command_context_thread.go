package cli

import (
	"context"
	"fmt"
	"strings"

	localinbox "github.com/iwen-conf/aitask-cli/internal/inbox"
	"github.com/spf13/cobra"
)

func newContextThreadCommand(env *CommandEnv) *cobra.Command {
	return &cobra.Command{
		Use:   "thread <thread_id>",
		Short: "Render local context for a thread",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runContextThread(cmd.Context(), env, args[0])
		},
	}
}

func runContextThread(parent context.Context, env *CommandEnv, threadID string) error {
	ctx, cancel := context.WithTimeout(parent, env.opts.timeout)
	defer cancel()
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return fmt.Errorf("thread id cannot be empty")
	}
	db, closeDB, err := openInboxQueryDB(ctx)
	if err != nil {
		return err
	}
	defer closeDB()
	events, err := localinbox.ListThread(ctx, db, threadID)
	if err != nil {
		return err
	}
	summary, _ := readSummaryRow(ctx, "thread", threadID)
	payload := map[string]any{
		"threadId": threadID,
		"summary":  summary,
		"events":   events,
	}
	return env.printer().Print(RenderData{
		Brief:  fmt.Sprintf("%d thread event(s)", len(events)),
		Prompt: renderContextThreadPrompt(threadID, summary, events),
		JSON:   payload,
	})
}

func renderContextThreadPrompt(threadID string, summary summaryRow, events []localinbox.EventRow) string {
	lines := []string{"# Thread Context", "", "Thread: `" + threadID + "`", ""}
	if strings.TrimSpace(summary.Summary) != "" {
		lines = append(lines, "## Summary", "", summary.Summary, "")
	}
	lines = append(lines, "## Events", "")
	if len(events) == 0 {
		lines = append(lines, "(empty)")
		return strings.Join(lines, "\n")
	}
	for _, event := range events {
		lines = append(lines, fmt.Sprintf("- [%s] %s `%s` from `%s`: %s", event.CreatedAt, event.Kind, event.ID, event.FromAgent, compactInboxBody(event.Body)))
	}
	return strings.Join(lines, "\n")
}

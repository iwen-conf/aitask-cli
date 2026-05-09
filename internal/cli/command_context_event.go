package cli

import (
	"context"
	"fmt"
	"strings"

	localwatch "github.com/iwen-conf/aitask-cli/internal/agentwatch"
	"github.com/spf13/cobra"
)

func newContextEventCommand(env *CommandEnv) *cobra.Command {
	return &cobra.Command{
		Use:   "event <event_id>",
		Short: "Render local context and memory recall for an event",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runContextEvent(cmd.Context(), env, args[0])
		},
	}
}

func runContextEvent(parent context.Context, env *CommandEnv, eventID string) error {
	ctx, cancel := context.WithTimeout(parent, env.opts.timeout)
	defer cancel()
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return fmt.Errorf("event id cannot be empty")
	}
	db, closeDB, err := openInboxQueryDB(ctx)
	if err != nil {
		return err
	}
	defer closeDB()
	event, err := localwatch.LoadPromptEvent(ctx, db, eventID)
	if err != nil {
		return err
	}
	agent := effectiveInboxAgent(env, "")
	recall := ""
	if recaller := buildBackendRecaller(env); recaller != nil {
		recall, _ = recaller.Recall(ctx, agent, eventID)
	}
	payload := map[string]any{
		"eventId": eventID,
		"agent":   agent,
		"event":   event,
		"recall":  recall,
	}
	if strings.TrimSpace(event.ThreadID) != "" {
		summary, _ := readSummaryRow(ctx, "thread", event.ThreadID)
		payload["threadSummary"] = summary
		return env.printer().Print(RenderData{
			Brief:  fmt.Sprintf("event %s", eventID),
			Prompt: renderContextEventPrompt(event, recall, summary),
			JSON:   payload,
		})
	}
	return env.printer().Print(RenderData{
		Brief:  fmt.Sprintf("event %s", eventID),
		Prompt: renderContextEventPrompt(event, recall, summaryRow{}),
		JSON:   payload,
	})
}

func renderContextEventPrompt(event localwatch.PromptEvent, recall string, summary summaryRow) string {
	lines := []string{
		"# Event Context",
		"",
		"Event: `" + contextValueOrUnknown(event.ID) + "`",
		"Kind: `" + contextValueOrUnknown(event.Kind) + "`",
		"From: `" + contextValueOrUnknown(event.From) + "`",
		"Project: `" + contextValueOrUnknown(event.Project) + "`",
		"Thread: `" + contextValueOrUnknown(event.ThreadID) + "`",
		"Created At: `" + contextValueOrUnknown(event.CreatedAt) + "`",
		"",
		"## Event Body",
		"",
		fallback(event.Body, "(empty)"),
	}
	if strings.TrimSpace(summary.Summary) != "" {
		lines = append(lines, "", "## Thread Summary", "", summary.Summary)
	}
	lines = append(lines, "", "## Memory Recall", "")
	if strings.TrimSpace(recall) == "" {
		lines = append(lines, "(none)")
	} else {
		lines = append(lines, strings.TrimSpace(recall))
	}
	return strings.Join(lines, "\n")
}

func contextValueOrUnknown(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "(unknown)"
	}
	return value
}

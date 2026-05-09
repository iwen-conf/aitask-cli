package cli

import (
	"fmt"
	"strings"

	localwatch "github.com/iwen-conf/aitask-cli/internal/agentwatch"
	"github.com/spf13/cobra"
)

type renderPromptOptions struct {
	eventID  string
	agent    string
	noRecall bool
}

func newRenderPromptCommand(env *CommandEnv) *cobra.Command {
	opts := &renderPromptOptions{}
	cmd := &cobra.Command{
		Use:   "render-prompt",
		Short: "Render a local inbox event prompt",
		RunE: func(_ *cobra.Command, _ []string) error {
			if strings.TrimSpace(opts.eventID) == "" {
				return fmt.Errorf("--event is required")
			}
			ctx, cancel := env.context()
			defer cancel()
			db, closeDB, err := openInboxQueryDB(ctx)
			if err != nil {
				return err
			}
			defer closeDB()
			agent := effectiveInboxAgent(env, opts.agent)
			evt, err := localwatch.LoadPromptEvent(ctx, db, opts.eventID)
			if err != nil {
				return err
			}
			recall := ""
			if !opts.noRecall {
				if recaller := buildBackendRecaller(env); recaller != nil {
					recall, _ = recaller.Recall(ctx, agent, opts.eventID)
				}
			}
			prompt := localwatch.RenderPrompt(opts.eventID, agent, recall, evt)
			return env.printer().Print(RenderData{
				Brief:  opts.eventID,
				Prompt: prompt,
				JSON: map[string]any{
					"eventId": opts.eventID,
					"agent":   agent,
					"prompt":  prompt,
				},
			})
		},
	}
	cmd.Flags().StringVar(&opts.eventID, "event", "", "event id")
	cmd.Flags().StringVar(&opts.agent, "agent", "", "agent name (default: current profile)")
	cmd.Flags().BoolVar(&opts.noRecall, "no-recall", false, "disable backend memory recall")
	return cmd
}

func buildBackendRecaller(env *CommandEnv) *backendRecaller {
	cfg, err := env.resolveProjectConfig(false)
	if err != nil || strings.TrimSpace(cfg.ProjectID) == "" {
		return nil
	}
	client, _, err := env.clientWithToken(false)
	if err != nil {
		return nil
	}
	return &backendRecaller{client: client, projectID: cfg.ProjectID}
}

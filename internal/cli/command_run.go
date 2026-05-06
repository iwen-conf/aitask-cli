package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func newRunCommand(env *CommandEnv) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Agent run operations",
	}
	cmd.AddCommand(newRunEndCommand(env))
	return cmd
}

func newRunEndCommand(env *CommandEnv) *cobra.Command {
	var reason string
	var taskID string
	var runID string
	cmd := &cobra.Command{
		Use:   "end",
		Short: "End current active run explicitly",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := env.resolveProjectConfig(true)
			if err != nil {
				return err
			}
			client, _, err := env.clientWithToken(true)
			if err != nil {
				return err
			}
			ctx, cancel := env.context()
			defer cancel()

			resolvedTaskID := strings.TrimSpace(taskID)
			resolvedRunID := strings.TrimSpace(runID)
			if resolvedTaskID == "" || resolvedRunID == "" {
				current, currentErr := client.GetCurrentTask(ctx, cfg.ProjectID)
				if currentErr != nil {
					return currentErr
				}
				if current.GetTask() == nil {
					return fmt.Errorf("no current task found")
				}
				if resolvedTaskID == "" {
					resolvedTaskID = strings.TrimSpace(current.GetTask().GetTaskId())
				}
				if resolvedRunID == "" {
					resolvedRunID = strings.TrimSpace(current.GetTask().GetActiveRunId())
				}
			}
			if resolvedTaskID == "" {
				return fmt.Errorf("task id cannot be empty")
			}
			if resolvedRunID == "" {
				return fmt.Errorf("active run id cannot be empty, use --run or start task first")
			}

			payload, err := client.PostREST(ctx, "/api/projects/"+cfg.ProjectID+"/tasks/"+resolvedTaskID+"/fail", map[string]any{
				"runId":  resolvedRunID,
				"reason": strings.TrimSpace(reason),
			})
			if err != nil {
				return err
			}

			safeWriteLastSync(cfg.AITaskDir, "run end", "online", false)
			jsonOut := map[string]any{
				"projectId": cfg.ProjectID,
				"taskId":    resolvedTaskID,
				"runId":     resolvedRunID,
				"status":    mapString(payload, "status"),
				"reason":    strings.TrimSpace(reason),
			}
			prompt := fmt.Sprintf("# Run Ended\n\nTask `%s` run `%s` ended with status `%s`.\nReason: `%s`", resolvedTaskID, resolvedRunID, fallback(mapString(payload, "status"), "failed"), strings.TrimSpace(reason))
			return env.printer().Print(RenderData{Brief: "run ended", Prompt: prompt, JSON: jsonOut})
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "context_limit_handoff", "end reason")
	cmd.Flags().StringVar(&taskID, "task", "", "task id (default current task)")
	cmd.Flags().StringVar(&runID, "run", "", "run id (default current active run)")
	return cmd
}

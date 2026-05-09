package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	aitaskv1 "github.com/iwen-conf/aitask-cli/internal/rpc/gen/aitask/v1"
)

const defaultContextMaxTokens = 200_000

func newContextCommand(env *CommandEnv) *cobra.Command {
	var threadID string
	var eventID string
	cmd := &cobra.Command{
		Use:   "context",
		Short: "Context lifecycle operations",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(threadID) != "" && strings.TrimSpace(eventID) != "" {
				return fmt.Errorf("--thread and --event are mutually exclusive")
			}
			if strings.TrimSpace(eventID) != "" {
				return runContextEvent(cmd.Context(), env, eventID)
			}
			if strings.TrimSpace(threadID) != "" {
				return runContextThread(cmd.Context(), env, threadID)
			}
			return cmd.Help()
		},
	}
	cmd.Flags().StringVar(&threadID, "thread", "", "render local context for a thread")
	cmd.Flags().StringVar(&eventID, "event", "", "render local context and memory recall for an event")
	cmd.AddCommand(
		newContextStatusCommand(env),
		newContextReportCommand(env),
		newContextCompactCommand(env),
		newContextHandoffCommand(env),
		newContextEventCommand(env),
		newContextThreadCommand(env),
	)
	return cmd
}

func newContextStatusCommand(env *CommandEnv) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show current context budget status",
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

			res, err := client.Bootstrap(ctx, cfg.ProjectID)
			if err != nil {
				safeWriteLastSync(cfg.AITaskDir, "context status", "offline", false)
				return err
			}
			budget := res.GetRun().GetContextBudget()
			jsonOut := map[string]any{
				"projectId": cfg.ProjectID,
				"runId":     res.GetRun().GetRunId(),
				"budget": map[string]any{
					"maxContextTokens":    budget.GetMaxContextTokens(),
					"estimatedUsedTokens": budget.GetEstimatedUsedTokens(),
					"state":               budget.GetState(),
					"usageRatio":          budget.GetUsageRatio(),
				},
				"nextAction": map[string]any{
					"type":    res.GetNextAction().GetType(),
					"message": res.GetNextAction().GetMessage(),
					"command": res.GetNextAction().GetCommand(),
				},
			}
			if err := writeStateJSON(cfg.AITaskDir, stateContextUsagePB, jsonOut); err != nil {
				return err
			}
			safeWriteLastSync(cfg.AITaskDir, "context status", "online", false)

			prompt := renderContextStatusPrompt(jsonOut)
			return env.printer().Print(RenderData{
				Brief:  fmt.Sprintf("%s %.2f%%", budget.GetState(), budget.GetUsageRatio()*100),
				Prompt: prompt,
				JSON:   jsonOut,
			})
		},
	}
}

func newContextReportCommand(env *CommandEnv) *cobra.Command {
	var inputTokens int
	var outputTokens int
	var maxTokens int
	var runID string
	var source string
	cmd := &cobra.Command{
		Use:   "report",
		Short: "Report context token usage",
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

			resolvedRunID := strings.TrimSpace(runID)
			resolvedMax := maxTokens
			if resolvedRunID == "" || resolvedMax <= 0 {
				boot, bootErr := client.Bootstrap(ctx, cfg.ProjectID)
				if bootErr != nil {
					safeWriteLastSync(cfg.AITaskDir, "context report", "offline", false)
					return bootErr
				}
				if resolvedRunID == "" {
					resolvedRunID = strings.TrimSpace(boot.GetRun().GetRunId())
				}
				if resolvedMax <= 0 {
					resolvedMax = int(boot.GetRun().GetContextBudget().GetMaxContextTokens())
				}
			}
			if resolvedRunID == "" {
				return fmt.Errorf("no active run found, start task first")
			}
			if resolvedMax <= 0 {
				resolvedMax = defaultContextMaxTokens
			}

			req := &aitaskv1.ReportRequest{
				ProjectId:            cfg.ProjectID,
				RunId:                resolvedRunID,
				ReportedInputTokens:  int32(inputTokens),
				ReportedOutputTokens: int32(outputTokens),
				MaxContextTokens:     int32(resolvedMax),
				Source:               strings.TrimSpace(source),
			}
			res, err := client.ReportContext(ctx, req)
			if err != nil {
				safeWriteLastSync(cfg.AITaskDir, "context report", "offline", false)
				return err
			}

			jsonOut := map[string]any{
				"projectId": cfg.ProjectID,
				"runId":     resolvedRunID,
				"budget": map[string]any{
					"maxContextTokens":    res.GetBudget().GetMaxContextTokens(),
					"estimatedUsedTokens": res.GetBudget().GetEstimatedUsedTokens(),
					"state":               res.GetBudget().GetState(),
					"usageRatio":          res.GetBudget().GetUsageRatio(),
				},
				"warnings": res.GetWarnings(),
				"nextAction": map[string]any{
					"type":    res.GetNextAction().GetType(),
					"message": res.GetNextAction().GetMessage(),
					"command": res.GetNextAction().GetCommand(),
				},
			}
			if err := writeStateJSON(cfg.AITaskDir, stateContextUsagePB, jsonOut); err != nil {
				return err
			}
			safeWriteLastSync(cfg.AITaskDir, "context report", "online", false)

			return env.printer().Print(RenderData{
				Brief:  fmt.Sprintf("%s %.2f%%", res.GetBudget().GetState(), res.GetBudget().GetUsageRatio()*100),
				Prompt: renderContextReportPrompt(jsonOut),
				JSON:   jsonOut,
				Proto:  res,
			})
		},
	}
	cmd.Flags().IntVar(&inputTokens, "input", 0, "reported input tokens")
	cmd.Flags().IntVar(&outputTokens, "output", 0, "reported output tokens")
	cmd.Flags().IntVar(&maxTokens, "max", 0, "max context tokens (default from active run)")
	cmd.Flags().StringVar(&runID, "run", "", "run id (default active run)")
	cmd.Flags().StringVar(&source, "source", "", "report source (default agent type)")
	return cmd
}

func newContextCompactCommand(env *CommandEnv) *cobra.Command {
	return &cobra.Command{
		Use:   "compact",
		Short: "Compact context into refs-only output",
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

			payload, err := client.GetREST(ctx, "/api/projects/"+cfg.ProjectID+"/memory/search", map[string]string{
				"q":        "project summary decisions blockers handoff refs",
				"budget":   "1200",
				"refsOnly": "true",
			})
			if err != nil {
				safeWriteLastSync(cfg.AITaskDir, "context compact", "offline", false)
				return err
			}
			refsPayload := compactToRefs(payload)
			if err := writeStateJSON(cfg.AITaskDir, stateContextUsagePB, refsPayload); err != nil {
				return err
			}
			safeWriteLastSync(cfg.AITaskDir, "context compact", "online", false)

			return env.printer().Print(RenderData{
				Brief:  fmt.Sprintf("%d refs", len(asSlice(refsPayload["refs"]))),
				Prompt: renderContextCompactPrompt(refsPayload),
				JSON:   refsPayload,
			})
		},
	}
}

func newContextHandoffCommand(env *CommandEnv) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "handoff",
		Short: "Context handoff operations",
	}
	cmd.AddCommand(
		newContextHandoffPrepareCommand(env),
		newContextHandoffSubmitCommand(env),
		newContextHandoffCurrentCommand(env),
	)
	return cmd
}

func newContextHandoffPrepareCommand(env *CommandEnv) *cobra.Command {
	return &cobra.Command{
		Use:   "prepare",
		Short: "Generate .aitask/handoff.md template",
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
			safeWriteLastSync(cfg.AITaskDir, "context handoff prepare", "offline", false)

			current, _ := client.GetCurrentTask(ctx, cfg.ProjectID)
			taskID := ""
			taskTitle := ""
			runID := ""
			if current != nil && current.GetTask() != nil {
				taskID = current.GetTask().GetTaskId()
				taskTitle = current.GetTask().GetTitle()
				runID = current.GetTask().GetActiveRunId()
			}
			boot, _ := client.Bootstrap(ctx, cfg.ProjectID)
			state := "normal"
			if boot != nil {
				state = boot.GetRun().GetContextBudget().GetState()
				if runID == "" {
					runID = boot.GetRun().GetRunId()
				}
			}

			template := renderHandoffTemplate(cfg.ProjectID, taskID, taskTitle, runID, state)
			target := filepath.Join(cfg.AITaskDir, "handoff.md")
			if err := writeTextFile(target, template); err != nil {
				return err
			}
			safeWriteLastSync(cfg.AITaskDir, "context handoff prepare", "online", false)

			jsonOut := map[string]any{
				"projectId": cfg.ProjectID,
				"taskId":    taskID,
				"runId":     runID,
				"state":     state,
				"file":      target,
			}
			prompt := fmt.Sprintf("# Handoff Template Ready\n\nFile: `%s`\nTask: `%s`\nRun: `%s`\nState: `%s`\n", target, fallback(taskID, "(unknown)"), fallback(runID, "(unknown)"), state)
			return env.printer().Print(RenderData{Brief: "handoff template ready", Prompt: prompt, JSON: jsonOut})
		},
	}
}

func newContextHandoffSubmitCommand(env *CommandEnv) *cobra.Command {
	var from string
	var taskID string
	var reason string
	cmd := &cobra.Command{
		Use:   "submit",
		Short: "Submit handoff markdown",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := env.resolveProjectConfig(true)
			if err != nil {
				return err
			}
			file := strings.TrimSpace(from)
			if file == "" {
				file = ".aitask/handoff.md"
			}
			markdown, err := readTextFile(file)
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
			if resolvedTaskID == "" {
				current, currentErr := client.GetCurrentTask(ctx, cfg.ProjectID)
				if currentErr != nil {
					safeWriteLastSync(cfg.AITaskDir, "context handoff submit", "offline", false)
					return currentErr
				}
				if current.GetTask() == nil {
					return fmt.Errorf("no current task to attach handoff, use --task")
				}
				resolvedTaskID = current.GetTask().GetTaskId()
			}

			res, err := client.CreateHandoffRPC(ctx, &aitaskv1.CreateHandoffRequest{
				ProjectId:       cfg.ProjectID,
				TaskId:          resolvedTaskID,
				Reason:          strings.TrimSpace(reason),
				HandoffMarkdown: markdown,
			})
			if err != nil {
				safeWriteLastSync(cfg.AITaskDir, "context handoff submit", "offline", false)
				return err
			}

			jsonOut := map[string]any{
				"projectId":     cfg.ProjectID,
				"taskId":        resolvedTaskID,
				"handoffId":     res.GetHandoffId(),
				"openvikingUri": res.GetOpenvikingUri(),
				"nextAction": map[string]any{
					"type":    res.GetNextAction().GetType(),
					"message": res.GetNextAction().GetMessage(),
					"command": res.GetNextAction().GetCommand(),
				},
			}
			if err := writeStateJSON(cfg.AITaskDir, stateTaskDelegationPB, jsonOut); err != nil {
				return err
			}
			safeWriteLastSync(cfg.AITaskDir, "context handoff submit", "online", false)

			prompt := fmt.Sprintf("# Handoff Submitted\n\nHandoff ID: `%s`\nTask: `%s`\nOpenViking URI: `%s`\n\nNext:\n\n```bash\n%s\n```", res.GetHandoffId(), resolvedTaskID, res.GetOpenvikingUri(), res.GetNextAction().GetCommand())
			return env.printer().Print(RenderData{Brief: res.GetHandoffId(), Prompt: prompt, JSON: jsonOut, Proto: res})
		},
	}
	cmd.Flags().StringVar(&from, "from", ".aitask/handoff.md", "handoff markdown file")
	cmd.Flags().StringVar(&taskID, "task", "", "task id (default current task)")
	cmd.Flags().StringVar(&reason, "reason", "context_limit_handoff", "handoff reason")
	return cmd
}

func newContextHandoffCurrentCommand(env *CommandEnv) *cobra.Command {
	return &cobra.Command{
		Use:   "current",
		Short: "Show current unconsumed handoff",
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

			res, err := client.GetCurrentHandoffRPC(ctx, cfg.ProjectID)
			if err != nil {
				safeWriteLastSync(cfg.AITaskDir, "context handoff current", "offline", false)
				return err
			}
			jsonOut := map[string]any{
				"handoffId":       res.GetHandoffId(),
				"taskId":          res.GetTaskId(),
				"summary":         res.GetSummary(),
				"handoffMarkdown": res.GetHandoffMarkdown(),
				"contextRefs":     contextRefsToJSON(res.GetContextRefs()),
			}
			if err := writeStateJSON(cfg.AITaskDir, stateTaskDelegationPB, jsonOut); err != nil {
				return err
			}
			safeWriteLastSync(cfg.AITaskDir, "context handoff current", "online", false)

			prompt := renderCurrentHandoffPrompt(jsonOut)
			return env.printer().Print(RenderData{Brief: res.GetHandoffId(), Prompt: prompt, JSON: jsonOut, Proto: res})
		},
	}
}

func renderContextStatusPrompt(payload map[string]any) string {
	budget := asMap(payload["budget"])
	nextAction := asMap(payload["nextAction"])
	return fmt.Sprintf(
		"# Context Status\n\n- State: `%s`\n- Used Tokens: `%d`\n- Max Tokens: `%d`\n- Usage: `%.2f%%`\n\nNext:\n\n```bash\n%s\n```",
		mapString(budget, "state"),
		mapInt(budget, "estimatedUsedTokens"),
		mapInt(budget, "maxContextTokens"),
		mapFloat64(budget, "usageRatio")*100,
		fallback(mapString(nextAction, "command"), "aitask task current"),
	)
}

func renderContextReportPrompt(payload map[string]any) string {
	budget := asMap(payload["budget"])
	nextAction := asMap(payload["nextAction"])
	return fmt.Sprintf(
		"# Context Reported\n\n- State: `%s`\n- Used Tokens: `%d`\n- Max Tokens: `%d`\n- Usage: `%.2f%%`\n\nNext:\n\n```bash\n%s\n```",
		mapString(budget, "state"),
		mapInt(budget, "estimatedUsedTokens"),
		mapInt(budget, "maxContextTokens"),
		mapFloat64(budget, "usageRatio")*100,
		fallback(mapString(nextAction, "command"), "aitask task current"),
	)
}

func compactToRefs(payload map[string]any) map[string]any {
	items := asSlice(payload["items"])
	refs := make([]map[string]any, 0, len(items))
	for _, item := range items {
		entry := asMap(item)
		refs = append(refs, map[string]any{
			"uri":             mapString(entry, "uri"),
			"title":           mapString(entry, "title"),
			"estimatedTokens": mapInt(entry, "estimatedTokens"),
		})
	}
	return map[string]any{
		"refs": refs,
	}
}

func renderContextCompactPrompt(payload map[string]any) string {
	refs := asSlice(payload["refs"])
	lines := []string{"# Context Compact Refs", ""}
	if len(refs) == 0 {
		lines = append(lines, "(empty)")
		return strings.Join(lines, "\n")
	}
	for _, item := range refs {
		entry := asMap(item)
		lines = append(lines, fmt.Sprintf("- %s (%s)", mapString(entry, "title"), mapString(entry, "uri")))
	}
	return strings.Join(lines, "\n")
}

func contextRefsToJSON(items []*aitaskv1.ContextRef) []map[string]any {
	if len(items) == 0 {
		return []map[string]any{}
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{
			"uri":             item.GetUri(),
			"title":           item.GetTitle(),
			"estimatedTokens": item.GetEstimatedTokens(),
		})
	}
	return out
}

func renderCurrentHandoffPrompt(payload map[string]any) string {
	refs := asSlice(payload["contextRefs"])
	lines := []string{
		"# Current Handoff",
		"",
		"ID: `" + mapString(payload, "handoffId") + "`",
		"Task: `" + mapString(payload, "taskId") + "`",
		"",
		"## Summary",
		"",
		fallback(mapString(payload, "summary"), "(empty)"),
		"",
		"## Context Refs",
		"",
	}
	if len(refs) == 0 {
		lines = append(lines, "(none)")
	} else {
		for _, item := range refs {
			entry := asMap(item)
			lines = append(lines, "- "+mapString(entry, "title")+" ("+mapString(entry, "uri")+")")
		}
	}
	return strings.Join(lines, "\n")
}

func renderHandoffTemplate(projectID string, taskID string, taskTitle string, runID string, state string) string {
	taskLabel := taskID
	if taskLabel == "" {
		taskLabel = "(unknown)"
	}
	title := taskTitle
	if title == "" {
		title = "(unknown)"
	}
	return fmt.Sprintf(`# Context Handoff

project_id: %s
task_id: %s
task_title: %s
run_id: %s
context_state: %s

## What Was Done

- 

## Current Status

- 

## Blockers

- 

## Next Steps

1. 
2. 

## Key Refs

- viking://...
`, projectID, taskLabel, title, fallback(runID, "(unknown)"), state)
}

package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	aitaskv1 "github.com/iwen-conf/aitask-cli/internal/rpc/gen/aitask/v1"
	"github.com/iwen-conf/aitask-cli/pkg/ids"
)

func newTaskCommand(env *CommandEnv) *cobra.Command {
	cmd := &cobra.Command{Use: "task", Short: "Delegated task operations"}

	cmd.AddCommand(
		newTaskCurrentCommand(env),
		newTaskInboxCommand(env),
		newTaskDetailCommand(env),
		newTaskStartCommand(env),
		newTaskHeartbeatCommand(env),
		newTaskCheckpointCommand(env),
		newTaskSubmitCommand(env),
		newTaskFailCommand(env),
		newTaskReviewCommand(env),
		newTaskCreateCommand(env),
		newTaskResumeCommand(env),
	)
	return cmd
}

func newTaskCurrentCommand(env *CommandEnv) *cobra.Command {
	return &cobra.Command{
		Use:   "current",
		Short: "Get current delegated task",
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
			res, err := client.GetCurrentTask(ctx, cfg.ProjectID)
			if err != nil {
				// 离线降级：已有 current-task.md 时直接返回缓存内容，并提示离线。
				if cached, readErr := readTextFile(filepath.Join(cfg.AITaskDir, "current-task.md")); readErr == nil && strings.TrimSpace(cached) != "" {
					safeWriteLastSync(cfg.AITaskDir, "task current", "offline", false)
					prompt := "# Current Task (Offline)\n\nBackend unreachable, fallback to local cache.\n\n" + cached
					return env.printer().Print(RenderData{
						Brief:  "offline cache",
						Prompt: prompt,
						JSON: map[string]any{
							"offline": true,
							"source":  filepath.Join(cfg.AITaskDir, "current-task.md"),
							"content": cached,
						},
					})
				}
				safeWriteLastSync(cfg.AITaskDir, "task current", "offline", false)
				return err
			}
			if res.GetTask() != nil {
				md := renderCurrentTaskPrompt(res.GetTask())
				_ = writeTextFile(filepath.Join(cfg.AITaskDir, "current-task.md"), md)
				_ = writeStateJSON(cfg.AITaskDir, stateCurrentTaskPB, currentTaskToJSON(res))
				_ = writeStateJSON(cfg.AITaskDir, stateTaskDelegationPB, map[string]any{
					"taskId":            res.GetTask().GetTaskId(),
					"projectId":         res.GetTask().GetProjectId(),
					"status":            res.GetTask().GetStatus(),
					"assigneeAgentId":   res.GetTask().GetAssigneeAgentId(),
					"assigneeAgentType": res.GetTask().GetAssigneeAgentType(),
					"activeRunId":       res.GetTask().GetActiveRunId(),
				})
			}
			safeWriteLastSync(cfg.AITaskDir, "task current", "online", false)
			return env.printer().Print(RenderData{Brief: renderCurrentTaskBrief(res), Prompt: renderCurrentTaskResponsePrompt(res), JSON: currentTaskToJSON(res), Proto: res})
		},
	}
}

func newTaskInboxCommand(env *CommandEnv) *cobra.Command {
	return &cobra.Command{
		Use:   "inbox",
		Short: "List delegated tasks for current agent",
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
			whoami, err := client.WhoAmI(ctx)
			if err != nil {
				return err
			}
			agentID := whoami.GetIdentity().GetAgentId()
			payload, err := client.GetREST(ctx, "/api/projects/"+cfg.ProjectID+"/tasks", map[string]string{"status": "delegated", "assigneeAgentId": agentID})
			if err != nil {
				return err
			}
			return env.printer().Print(RenderData{Brief: fmt.Sprintf("%d tasks", len(asSlice(payload["items"]))), Prompt: renderTaskListPrompt("Delegated Inbox", asSlice(payload["items"])), JSON: payload})
		},
	}
}

func newTaskDetailCommand(env *CommandEnv) *cobra.Command {
	return &cobra.Command{
		Use:   "detail <task_id>",
		Short: "Get task detail",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
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
			payload, err := client.GetREST(ctx, "/api/projects/"+cfg.ProjectID+"/tasks/"+strings.TrimSpace(args[0]), nil)
			if err != nil {
				return err
			}
			return env.printer().Print(RenderData{Brief: mapString(payload, "taskId"), Prompt: renderTaskDetailPrompt(payload), JSON: payload})
		},
	}
}

func newTaskStartCommand(env *CommandEnv) *cobra.Command {
	var runID string
	cmd := &cobra.Command{
		Use:   "start <task_id>",
		Short: "Start delegated task",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cfg, err := env.resolveProjectConfig(true)
			if err != nil {
				return err
			}
			if strings.TrimSpace(runID) == "" {
				runID = ids.New(ids.PrefixRun)
			}
			client, _, err := env.clientWithToken(true)
			if err != nil {
				return err
			}
			ctx, cancel := env.context()
			defer cancel()
			res, err := client.StartTaskRPC(ctx, cfg.ProjectID, strings.TrimSpace(args[0]), runID)
			if err != nil {
				return err
			}
			jsonOut := map[string]any{"taskId": res.GetTaskId(), "status": res.GetStatus(), "activeRunId": res.GetActiveRunId(), "startedAt": res.GetStartedAt()}
			prompt := fmt.Sprintf("# Task Started\n\nTask `%s` started.\n\n- Status: `%s`\n- Run ID: `%s`", res.GetTaskId(), res.GetStatus(), res.GetActiveRunId())
			return env.printer().Print(RenderData{Brief: "started", Prompt: prompt, JSON: jsonOut, Proto: res})
		},
	}
	cmd.Flags().StringVar(&runID, "run", "", "run ID (default: auto-generated)")
	return cmd
}

func newTaskHeartbeatCommand(env *CommandEnv) *cobra.Command {
	var taskID string
	var runID string
	cmd := &cobra.Command{
		Use:   "heartbeat",
		Short: "Send heartbeat for running task",
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
			resolvedTaskID, resolvedRunID, err := resolveTaskAndRun(ctx, client, cfg.ProjectID, taskID, runID)
			if err != nil {
				return err
			}
			payload, err := client.PostREST(ctx, "/api/projects/"+cfg.ProjectID+"/tasks/"+resolvedTaskID+"/heartbeat", map[string]any{"runId": resolvedRunID})
			if err != nil {
				return err
			}
			prompt := fmt.Sprintf("# Heartbeat Sent\n\nTask `%s` heartbeat updated for run `%s`.", resolvedTaskID, resolvedRunID)
			return env.printer().Print(RenderData{Brief: "heartbeat updated", Prompt: prompt, JSON: payload})
		},
	}
	cmd.Flags().StringVar(&taskID, "task", "", "task ID (default: current task)")
	cmd.Flags().StringVar(&runID, "run", "", "run ID (default: task active run)")
	return cmd
}

func newTaskCheckpointCommand(env *CommandEnv) *cobra.Command {
	var taskID string
	var runID string
	var from string
	cmd := &cobra.Command{
		Use:   "checkpoint",
		Short: "Send checkpoint content through heartbeat",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := env.resolveProjectConfig(true)
			if err != nil {
				return err
			}
			if strings.TrimSpace(taskID) == "" {
				return fmt.Errorf("--task is required")
			}
			content, err := readTextFile(strings.TrimSpace(from))
			if err != nil {
				return err
			}
			client, _, err := env.clientWithToken(true)
			if err != nil {
				return err
			}
			ctx, cancel := env.context()
			defer cancel()
			_, resolvedRunID, err := resolveTaskAndRun(ctx, client, cfg.ProjectID, taskID, runID)
			if err != nil {
				return err
			}
			payload, err := client.PostREST(ctx, "/api/projects/"+cfg.ProjectID+"/tasks/"+taskID+"/heartbeat", map[string]any{"runId": resolvedRunID, "checkpoint": content})
			if err != nil {
				return err
			}
			prompt := fmt.Sprintf("# Checkpoint Uploaded\n\nTask `%s` checkpoint sent from `%s`.\n\nRun ID: `%s`", taskID, from, resolvedRunID)
			return env.printer().Print(RenderData{Brief: "checkpoint uploaded", Prompt: prompt, JSON: payload})
		},
	}
	cmd.Flags().StringVar(&taskID, "task", "", "task ID")
	cmd.Flags().StringVar(&runID, "run", "", "run ID")
	cmd.Flags().StringVar(&from, "from", ".aitask/progress.md", "checkpoint markdown path")
	return cmd
}

func newTaskSubmitCommand(env *CommandEnv) *cobra.Command {
	var from string
	var runID string
	var artifacts []string
	cmd := &cobra.Command{
		Use:   "submit <task_id>",
		Short: "Submit task result markdown",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cfg, err := env.resolveProjectConfig(true)
			if err != nil {
				return err
			}
			content, err := readTextFile(strings.TrimSpace(from))
			if err != nil {
				return err
			}
			client, _, err := env.clientWithToken(true)
			if err != nil {
				return err
			}
			ctx, cancel := env.context()
			defer cancel()
			taskID := strings.TrimSpace(args[0])
			_, resolvedRunID, err := resolveTaskAndRun(ctx, client, cfg.ProjectID, taskID, runID)
			if err != nil {
				return err
			}
			artifactRefs, err := parseArtifactFlags(artifacts)
			if err != nil {
				return err
			}
			res, err := client.SubmitTaskRPC(ctx, cfg.ProjectID, taskID, resolvedRunID, content, artifactRefs)
			if err != nil {
				return err
			}
			prompt := fmt.Sprintf("# Task Submitted\n\nTask `%s` submitted with status `%s`.\n\nNext: `%s`", res.GetTaskId(), res.GetStatus(), res.GetNextAction().GetCommand())
			jsonOut := map[string]any{"taskId": res.GetTaskId(), "status": res.GetStatus(), "nextAction": map[string]any{"type": res.GetNextAction().GetType(), "message": res.GetNextAction().GetMessage(), "command": res.GetNextAction().GetCommand()}}
			return env.printer().Print(RenderData{Brief: "submitted", Prompt: prompt, JSON: jsonOut, Proto: res})
		},
	}
	cmd.Flags().StringVar(&from, "from", ".aitask/result.md", "result markdown file")
	cmd.Flags().StringVar(&runID, "run", "", "run ID")
	cmd.Flags().StringSliceVar(&artifacts, "artifact", nil, "artifact in format type:name:uri (repeatable)")
	return cmd
}

func newTaskFailCommand(env *CommandEnv) *cobra.Command {
	var reason string
	var runID string
	cmd := &cobra.Command{
		Use:   "fail <task_id>",
		Short: "Mark task as failed",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cfg, err := env.resolveProjectConfig(true)
			if err != nil {
				return err
			}
			if strings.TrimSpace(reason) == "" {
				return fmt.Errorf("--reason is required")
			}
			client, _, err := env.clientWithToken(true)
			if err != nil {
				return err
			}
			ctx, cancel := env.context()
			defer cancel()
			taskID := strings.TrimSpace(args[0])
			_, resolvedRunID, err := resolveTaskAndRun(ctx, client, cfg.ProjectID, taskID, runID)
			if err != nil {
				return err
			}
			payload, err := client.PostREST(ctx, "/api/projects/"+cfg.ProjectID+"/tasks/"+taskID+"/fail", map[string]any{"runId": resolvedRunID, "reason": reason})
			if err != nil {
				return err
			}
			prompt := fmt.Sprintf("# Task Failed\n\nTask `%s` failed.\n\nReason: %s", taskID, reason)
			return env.printer().Print(RenderData{Brief: "failed", Prompt: prompt, JSON: payload})
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "failure reason")
	cmd.Flags().StringVar(&runID, "run", "", "run ID")
	return cmd
}

func newTaskReviewCommand(env *CommandEnv) *cobra.Command {
	var approve bool
	var reject bool
	var comment string
	var reason string
	cmd := &cobra.Command{
		Use:   "review <task_id>",
		Short: "Review submitted task",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cfg, err := env.resolveProjectConfig(true)
			if err != nil {
				return err
			}
			if approve == reject {
				return fmt.Errorf("exactly one of --approve or --reject is required")
			}
			reviewReason := strings.TrimSpace(comment)
			if reject {
				reviewReason = strings.TrimSpace(reason)
			}
			client, _, err := env.clientWithToken(true)
			if err != nil {
				return err
			}
			ctx, cancel := env.context()
			defer cancel()
			payload, err := client.PostREST(ctx, "/api/projects/"+cfg.ProjectID+"/tasks/"+strings.TrimSpace(args[0])+"/review", map[string]any{"approve": approve, "reason": reviewReason})
			if err != nil {
				return err
			}
			result := "approved"
			if reject {
				result = "rejected"
			}
			prompt := fmt.Sprintf("# Review Completed\n\nTask `%s` %s.", strings.TrimSpace(args[0]), result)
			return env.printer().Print(RenderData{Brief: result, Prompt: prompt, JSON: payload})
		},
	}
	cmd.Flags().BoolVar(&approve, "approve", false, "approve task")
	cmd.Flags().BoolVar(&reject, "reject", false, "reject task")
	cmd.Flags().StringVar(&comment, "comment", "", "review comment for approve path")
	cmd.Flags().StringVar(&reason, "reason", "", "review reason for reject path")
	return cmd
}

func newTaskCreateCommand(env *CommandEnv) *cobra.Command {
	var parent string
	var title string
	var description string
	var goal string
	var inputs string
	var constraints string
	var target string
	var skill string
	var model string
	var outputContract string
	var priority int
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create and optionally delegate task",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := env.resolveProjectConfig(true)
			if err != nil {
				return err
			}
			if strings.TrimSpace(title) == "" {
				return fmt.Errorf("--title is required")
			}
			client, _, err := env.clientWithToken(true)
			if err != nil {
				return err
			}
			ctx, cancel := env.context()
			defer cancel()

			delegateTo := ""
			delegateType := ""
			if strings.TrimSpace(target) != "" {
				delegateTo, delegateType, err = resolveDelegateTarget(ctx, client, cfg.ProjectID, target)
				if err != nil {
					return err
				}
			}

			body := map[string]any{
				"title":               title,
				"description":         description,
				"goal":                emptyAsNil(goal),
				"inputs":              emptyAsNil(inputs),
				"constraints":         emptyAsNil(constraints),
				"parentTaskId":        emptyAsNil(parent),
				"delegateToAgentId":   emptyAsNil(delegateTo),
				"delegateToAgentType": emptyAsNil(delegateType),
				"requiredModel":       emptyAsNil(model),
				"priority":            priority,
				"outputContract":      emptyAsNil(outputContract),
			}
			if strings.TrimSpace(skill) != "" {
				body["requiredSkills"] = []string{strings.TrimSpace(skill)}
			}
			payload, err := client.PostREST(ctx, "/api/projects/"+cfg.ProjectID+"/tasks", body)
			if err != nil {
				return err
			}
			prompt := fmt.Sprintf("# Task Created\n\nTask `%s` created with status `%s`.", mapString(payload, "taskId"), mapString(payload, "status"))
			return env.printer().Print(RenderData{Brief: "created", Prompt: prompt, JSON: payload})
		},
	}
	cmd.Flags().StringVar(&parent, "parent", "", "parent task ID")
	cmd.Flags().StringVar(&title, "title", "", "task title")
	cmd.Flags().StringVar(&description, "description", "", "task background / context")
	cmd.Flags().StringVar(&goal, "goal", "", "task goal (single sentence success criterion)")
	cmd.Flags().StringVar(&inputs, "inputs", "", "task inputs (resources, files, upstream APIs)")
	cmd.Flags().StringVar(&constraints, "constraints", "", "task constraints (what not to touch, compatibility limits)")
	cmd.Flags().StringVar(&target, "target", "", "target agent type or id (e.g. codex or agt_xxx)")
	cmd.Flags().StringVar(&skill, "skill", "", "required skill")
	cmd.Flags().StringVar(&model, "model", "", "required model")
	cmd.Flags().StringVar(&outputContract, "output-contract", "", "acceptance criteria / output contract markdown")
	cmd.Flags().IntVar(&priority, "priority", 0, "priority")
	return cmd
}

func newTaskResumeCommand(env *CommandEnv) *cobra.Command {
	var handoffID string
	var runID string
	cmd := &cobra.Command{
		Use:   "resume <task_id>",
		Short: "Resume task from handoff or heartbeat-timeout recovery",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cfg, err := env.resolveProjectConfig(true)
			if err != nil {
				return err
			}
			if strings.TrimSpace(runID) == "" {
				runID = ids.New(ids.PrefixRun)
			}
			client, _, err := env.clientWithToken(true)
			if err != nil {
				return err
			}
			ctx, cancel := env.context()
			defer cancel()
			payload, err := client.PostREST(ctx, "/api/projects/"+cfg.ProjectID+"/tasks/"+strings.TrimSpace(args[0])+"/resume", map[string]any{"handoffId": handoffID, "runId": runID})
			if err != nil {
				return err
			}
			mode := "heartbeat-timeout recovery"
			if strings.TrimSpace(handoffID) != "" {
				mode = "handoff"
			}
			prompt := fmt.Sprintf("# Task Resumed\n\nTask `%s` resumed with run `%s` via %s.", strings.TrimSpace(args[0]), runID, mode)
			return env.printer().Print(RenderData{Brief: "resumed", Prompt: prompt, JSON: payload})
		},
	}
	cmd.Flags().StringVar(&handoffID, "handoff", "", "handoff ID (optional for heartbeat-timeout recovery)")
	cmd.Flags().StringVar(&runID, "run", "", "new run ID")
	return cmd
}

func renderCurrentTaskBrief(res *aitaskv1.GetCurrentTaskResponse) string {
	if res.GetTask() == nil {
		return "no current task"
	}
	return res.GetTask().GetTaskId()
}

func renderCurrentTaskResponsePrompt(res *aitaskv1.GetCurrentTaskResponse) string {
	if res.GetTask() == nil {
		return fmt.Sprintf("# Current Task\n\nNo delegated task currently assigned.\n\nNext:\n\n`%s`", res.GetNextAction().GetCommand())
	}
	prompt := renderCurrentTaskPrompt(res.GetTask())
	if next := strings.TrimSpace(res.GetNextAction().GetCommand()); next != "" {
		prompt += "\n\nNext:\n\n```bash\n" + next + "\n```\n"
	}
	return prompt
}

func renderCurrentTaskPrompt(task *aitaskv1.Task) string {
	skill := "(none)"
	if len(task.GetRequiredSkills()) > 0 {
		skill = task.GetRequiredSkills()[0].GetName()
	}
	activeRun := task.GetActiveRunId()
	if strings.TrimSpace(activeRun) == "" {
		activeRun = "not started"
	}
	delegatedBy := task.GetDelegation().GetDelegatedByAgentId()
	if strings.TrimSpace(delegatedBy) == "" {
		delegatedBy = task.GetDelegation().GetDelegatedByType()
	}
	structured := renderTaskStructuredSections(task)
	return fmt.Sprintf(`# Current Task

Task ID: %s
Title: %s
Skill: %s
Delegated By: %s
Active Run: %s
%s
## Context

Read:

- .aitask/current-task.md
- .aitask/skills/%s.md

## Required Output

Write result to:

- .aitask/result.md

Then submit:

`+"```bash"+`
aitask task submit %s --from .aitask/result.md
`+"```"+`
`, task.GetTaskId(), task.GetTitle(), skill, delegatedBy, activeRun, structured, skill, task.GetTaskId())
}

// renderTaskStructuredSections emits the goal / background / inputs /
// constraints / acceptance blocks when they are non-empty. Each section starts
// with a leading newline so callers can drop the return value directly into
// templated output without worrying about spacing when all fields are blank.
func renderTaskStructuredSections(task *aitaskv1.Task) string {
	var b strings.Builder
	appendSection := func(heading string, body string) {
		body = strings.TrimSpace(body)
		if body == "" {
			return
		}
		b.WriteString("\n## ")
		b.WriteString(heading)
		b.WriteString("\n\n")
		b.WriteString(body)
		b.WriteString("\n")
	}
	appendSection("Goal", task.GetGoal())
	appendSection("Background", task.GetDescription())
	appendSection("Inputs", task.GetInputs())
	appendSection("Constraints", task.GetConstraints())
	appendSection("Acceptance Criteria", task.GetOutputContract())
	return b.String()
}

func currentTaskToJSON(res *aitaskv1.GetCurrentTaskResponse) map[string]any {
	out := map[string]any{"nextAction": map[string]any{"type": res.GetNextAction().GetType(), "message": res.GetNextAction().GetMessage(), "command": res.GetNextAction().GetCommand()}}
	if task := res.GetTask(); task != nil {
		skills := make([]string, 0, len(task.GetRequiredSkills()))
		for _, item := range task.GetRequiredSkills() {
			skills = append(skills, item.GetName())
		}
		out["task"] = map[string]any{
			"taskId":          task.GetTaskId(),
			"projectId":       task.GetProjectId(),
			"title":           task.GetTitle(),
			"status":          task.GetStatus(),
			"assigneeAgentId": task.GetAssigneeAgentId(),
			"activeRunId":     task.GetActiveRunId(),
			"requiredSkills":  skills,
			"requiredModel":   task.GetRequiredModel(),
			"outputContract":  task.GetOutputContract(),
			"goal":            task.GetGoal(),
			"description":     task.GetDescription(),
			"inputs":          task.GetInputs(),
			"constraints":     task.GetConstraints(),
		}
	}
	return out
}

func renderTaskListPrompt(title string, items []any) string {
	lines := []string{"# " + title, ""}
	if len(items) == 0 {
		lines = append(lines, "(empty)")
		return strings.Join(lines, "\n")
	}
	for _, item := range items {
		task := asMap(item)
		lines = append(lines, fmt.Sprintf("- %s | %s | %s", mapString(task, "taskId"), mapString(task, "status"), mapString(task, "title")))
	}
	return strings.Join(lines, "\n")
}

func renderTaskDetailPrompt(task map[string]any) string {
	header := fmt.Sprintf("# Task Detail\n\n- Task ID: `%s`\n- Title: %s\n- Status: `%s`\n- Assignee: `%s`\n- Active Run: `%s`\n- Priority: `%d`", mapString(task, "taskId"), mapString(task, "title"), mapString(task, "status"), mapString(task, "assigneeAgentId"), mapString(task, "activeRunId"), mapInt(task, "priority"))
	body := renderTaskStructuredSectionsFromMap(task)
	if strings.TrimSpace(body) == "" {
		// fallback to the legacy output-contract block when no structured field is filled.
		return header + "\n\n## Acceptance Criteria\n\n" + fallback(mapString(task, "outputContract"), "(none)")
	}
	return header + body
}

// renderTaskStructuredSectionsFromMap mirrors renderTaskStructuredSections but
// works against the loose map[string]any payload returned by REST endpoints.
func renderTaskStructuredSectionsFromMap(task map[string]any) string {
	var b strings.Builder
	appendSection := func(heading, body string) {
		body = strings.TrimSpace(body)
		if body == "" {
			return
		}
		b.WriteString("\n\n## ")
		b.WriteString(heading)
		b.WriteString("\n\n")
		b.WriteString(body)
	}
	appendSection("Goal", mapString(task, "goal"))
	appendSection("Background", mapString(task, "description"))
	appendSection("Inputs", mapString(task, "inputs"))
	appendSection("Constraints", mapString(task, "constraints"))
	appendSection("Acceptance Criteria", mapString(task, "outputContract"))
	return b.String()
}

func parseArtifactFlags(values []string) ([]*aitaskv1.ArtifactRef, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]*aitaskv1.ArtifactRef, 0, len(values))
	for _, item := range values {
		parts := strings.SplitN(strings.TrimSpace(item), ":", 3)
		if len(parts) != 3 {
			return nil, fmt.Errorf("invalid --artifact %q, expected type:name:uri", item)
		}
		out = append(out, &aitaskv1.ArtifactRef{ArtifactType: strings.TrimSpace(parts[0]), Name: strings.TrimSpace(parts[1]), Uri: strings.TrimSpace(parts[2])})
	}
	return out, nil
}

func resolveTaskAndRun(ctx context.Context, client *Client, projectID string, taskID string, runID string) (string, string, error) {
	resolvedTaskID := strings.TrimSpace(taskID)
	resolvedRunID := strings.TrimSpace(runID)
	if resolvedTaskID == "" {
		current, err := client.GetCurrentTask(ctx, projectID)
		if err != nil {
			return "", "", err
		}
		if current.GetTask() == nil {
			return "", "", fmt.Errorf("no current task available")
		}
		resolvedTaskID = current.GetTask().GetTaskId()
		if resolvedRunID == "" {
			resolvedRunID = strings.TrimSpace(current.GetTask().GetActiveRunId())
		}
	}
	if resolvedRunID == "" {
		payload, err := client.GetREST(ctx, "/api/projects/"+projectID+"/tasks/"+resolvedTaskID, nil)
		if err != nil {
			return "", "", err
		}
		resolvedRunID = mapString(payload, "activeRunId")
	}
	if resolvedRunID == "" {
		return "", "", fmt.Errorf("task %s has no active run id, start it first", resolvedTaskID)
	}
	return resolvedTaskID, resolvedRunID, nil
}

func resolveDelegateTarget(ctx context.Context, client *Client, projectID string, target string) (string, string, error) {
	target = strings.TrimSpace(target)
	if strings.HasPrefix(target, "agt_") {
		return target, "", nil
	}
	payload, err := client.GetREST(ctx, "/api/agents", nil)
	if err != nil {
		return "", "", err
	}
	for _, item := range asSlice(payload["items"]) {
		agent := asMap(item)
		if mapString(agent, "agentType") != target {
			continue
		}
		allowed := false
		for _, project := range asSlice(agent["boundProjects"]) {
			if strings.TrimSpace(fmt.Sprintf("%v", project)) == projectID {
				allowed = true
				break
			}
		}
		if allowed {
			return mapString(agent, "agentId"), target, nil
		}
	}
	return "", "", fmt.Errorf("no bound agent found for target %s in project %s", target, projectID)
}

func emptyAsNil(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}

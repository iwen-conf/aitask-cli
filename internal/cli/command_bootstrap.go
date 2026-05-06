package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	aitaskv1 "github.com/iwen-conf/aitask-cli/internal/rpc/gen/aitask/v1"
)

func newBootstrapCommand(env *CommandEnv) *cobra.Command {
	return &cobra.Command{
		Use:   "bootstrap",
		Short: "Load project bootstrap context",
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
				safeWriteLastSync(cfg.AITaskDir, "bootstrap", "offline", false)
				return err
			}

			contextMarkdown := renderBootstrapContextMarkdown(res)
			if err := writeTextFile(filepath.Join(cfg.AITaskDir, "context.md"), contextMarkdown); err != nil {
				return err
			}
			if err := writeProtoFile(filepath.Join(cfg.AITaskDir, "state", "bootstrap.pb"), res); err != nil {
				return err
			}
			if err := writeStateJSON(cfg.AITaskDir, stateRoomSnapshotPB, map[string]any{
				"projectId":      cfg.ProjectID,
				"roomId":         res.GetRoom().GetRoomId(),
				"recentSummary":  res.GetRoom().GetRecentSummary(),
				"unreadMentions": res.GetRoom().GetUnreadMentions(),
			}); err != nil {
				return err
			}
			if err := writeStateJSON(cfg.AITaskDir, stateContextUsagePB, map[string]any{
				"projectId": cfg.ProjectID,
				"runId":     res.GetRun().GetRunId(),
				"budget": map[string]any{
					"maxContextTokens":    res.GetRun().GetContextBudget().GetMaxContextTokens(),
					"estimatedUsedTokens": res.GetRun().GetContextBudget().GetEstimatedUsedTokens(),
					"state":               res.GetRun().GetContextBudget().GetState(),
					"usageRatio":          res.GetRun().GetContextBudget().GetUsageRatio(),
				},
			}); err != nil {
				return err
			}
			safeWriteLastSync(cfg.AITaskDir, "bootstrap", "online", false)

			jsonOut := bootstrapToJSON(res)
			return env.printer().Print(RenderData{
				Brief:  fmt.Sprintf("%s %s", res.GetProject().GetProjectId(), res.GetRun().GetContextBudget().GetState()),
				Prompt: renderBootstrapPrompt(res),
				JSON:   jsonOut,
				Proto:  res,
			})
		},
	}
}

func renderBootstrapContextMarkdown(res *aitaskv1.BootstrapResponse) string {
	refs := make([]string, 0, len(res.GetContextRefs()))
	for _, item := range res.GetContextRefs() {
		refs = append(refs, fmt.Sprintf("- %s (%s)", item.GetTitle(), item.GetUri()))
	}
	if len(refs) == 0 {
		refs = []string{"- (none)"}
	}
	return fmt.Sprintf(`# Context Snapshot

Project: %s
Project ID: %s
Session ID: %s
Run ID: %s
Context State: %s

## Context Refs

%s

## Room

- Room ID: %s
- Unread Mentions: %d

## Next Action

%s

Command:

`+"```bash"+`
%s
`+"```"+`
`,
		res.GetProject().GetName(),
		res.GetProject().GetProjectId(),
		res.GetSession().GetSessionId(),
		res.GetRun().GetRunId(),
		res.GetRun().GetContextBudget().GetState(),
		strings.Join(refs, "\n"),
		res.GetRoom().GetRoomId(),
		res.GetRoom().GetUnreadMentions(),
		res.GetNextAction().GetMessage(),
		res.GetNextAction().GetCommand(),
	)
}

func renderBootstrapPrompt(res *aitaskv1.BootstrapResponse) string {
	identity := res.GetIdentity()
	project := res.GetProject()
	run := res.GetRun()
	room := res.GetRoom()
	return fmt.Sprintf(`# AITask Bootstrap

You are %s.

Project: %s
Project ID: %s
Role: %s
Context State: %s

Current summary:
The project uses Task Orchestrator for task authority, OpenViking for memory, and Project Agent Room for collaboration.

Important rules:
- Do not rely on chat history.
- Do not start Codex or Gemini tasks unless explicitly delegated.
- Use CLI for all task state changes.
- Use OpenViking refs on demand instead of loading all context.

Room:
- Unread mentions: %d
- Recent summary: %s

Next command:

`+"```bash"+`
%s
`+"```"+`
`, identity.GetAgentId(), project.GetName(), project.GetProjectId(), identity.GetRole(), run.GetContextBudget().GetState(), room.GetUnreadMentions(), fallback(room.GetRecentSummary(), "(empty)"), res.GetNextAction().GetCommand())
}

func bootstrapToJSON(res *aitaskv1.BootstrapResponse) map[string]any {
	refs := make([]map[string]any, 0, len(res.GetContextRefs()))
	for _, ref := range res.GetContextRefs() {
		refs = append(refs, map[string]any{"uri": ref.GetUri(), "title": ref.GetTitle(), "estimatedTokens": ref.GetEstimatedTokens()})
	}
	return map[string]any{
		"identity": map[string]any{
			"agentId":         res.GetIdentity().GetAgentId(),
			"agentType":       res.GetIdentity().GetAgentType(),
			"role":            res.GetIdentity().GetRole(),
			"scopes":          res.GetIdentity().GetScopes(),
			"allowedProjects": res.GetIdentity().GetAllowedProjects(),
		},
		"project": map[string]any{"projectId": res.GetProject().GetProjectId(), "name": res.GetProject().GetName(), "status": res.GetProject().GetStatus()},
		"session": map[string]any{"sessionId": res.GetSession().GetSessionId(), "status": res.GetSession().GetStatus()},
		"run": map[string]any{
			"runId": res.GetRun().GetRunId(),
			"contextBudget": map[string]any{
				"maxContextTokens":    res.GetRun().GetContextBudget().GetMaxContextTokens(),
				"estimatedUsedTokens": res.GetRun().GetContextBudget().GetEstimatedUsedTokens(),
				"state":               res.GetRun().GetContextBudget().GetState(),
				"usageRatio":          res.GetRun().GetContextBudget().GetUsageRatio(),
			},
		},
		"contextRefs": refs,
		"room":        map[string]any{"roomId": res.GetRoom().GetRoomId(), "recentSummary": res.GetRoom().GetRecentSummary(), "unreadMentions": res.GetRoom().GetUnreadMentions()},
		"nextAction":  map[string]any{"type": res.GetNextAction().GetType(), "message": res.GetNextAction().GetMessage(), "command": res.GetNextAction().GetCommand()},
	}
}

func fallback(value string, fallbackValue string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallbackValue
	}
	return value
}

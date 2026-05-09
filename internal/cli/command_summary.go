package cli

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

type summaryCommandOptions struct {
	project bool
	agent   string
	thread  string
}

func newSummaryCommand(env *CommandEnv) *cobra.Command {
	opts := &summaryCommandOptions{}
	cmd := &cobra.Command{
		Use:   "summary",
		Short: "Show local or memory-backed summaries",
		RunE: func(_ *cobra.Command, _ []string) error {
			scope, scopeID, err := resolveSummaryScope(env, opts)
			if err != nil {
				return err
			}
			ctx, cancel := env.context()
			defer cancel()
			if row, err := readSummaryRow(ctx, scope, scopeID); err == nil {
				return env.printer().Print(RenderData{
					Brief:  fmt.Sprintf("%s:%s", row.Scope, row.ScopeID),
					Prompt: renderSummaryPrompt(row),
					JSON:   row,
				})
			} else if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			return renderSummaryFallback(ctx, env, scope, scopeID)
		},
	}
	cmd.Flags().BoolVar(&opts.project, "project", false, "show current project summary")
	cmd.Flags().StringVar(&opts.agent, "agent", "", "show agent summary")
	cmd.Flags().StringVar(&opts.thread, "thread", "", "show thread summary")
	return cmd
}

func resolveSummaryScope(env *CommandEnv, opts *summaryCommandOptions) (string, string, error) {
	count := 0
	if opts.project {
		count++
	}
	if strings.TrimSpace(opts.agent) != "" {
		count++
	}
	if strings.TrimSpace(opts.thread) != "" {
		count++
	}
	if count != 1 {
		return "", "", fmt.Errorf("exactly one of --project, --agent, or --thread is required")
	}
	if opts.project {
		cfg, err := env.resolveProjectConfig(true)
		if err != nil {
			return "", "", err
		}
		return "project", cfg.ProjectID, nil
	}
	if agent := strings.TrimSpace(opts.agent); agent != "" {
		return "agent", agent, nil
	}
	return "thread", strings.TrimSpace(opts.thread), nil
}

package cli

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	localinbox "github.com/iwen-conf/aitask-cli/internal/inbox"
	localstate "github.com/iwen-conf/aitask-cli/internal/state"
	"github.com/spf13/cobra"
)

type inboxCommandOptions struct {
	agent  string
	global bool
	status string
	limit  int
	reason string
	error  string
}

func newInboxCommand(env *CommandEnv) *cobra.Command {
	opts := &inboxCommandOptions{limit: 20}
	cmd := &cobra.Command{
		Use:   "inbox",
		Short: "List local agent inbox messages",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := env.context()
			defer cancel()
			db, closeDB, err := openInboxQueryDB(ctx)
			if err != nil {
				return err
			}
			defer closeDB()
			if opts.global {
				projectID := ""
				if cfg, err := env.resolveProjectConfig(false); err == nil {
					projectID = cfg.ProjectID
				}
				rows, err := localinbox.ListGlobalFeed(ctx, db, localinbox.ListOpts{Limit: opts.limit, Project: projectID})
				if err != nil {
					return err
				}
				return env.printer().Print(RenderData{
					Brief:  fmt.Sprintf("%d global item(s)", len(rows)),
					Prompt: renderInboxRowsPrompt("Global Inbox", rows),
					JSON:   rows,
				})
			}
			agent := effectiveInboxAgent(env, opts.agent)
			rows, err := localinbox.ListAgentInbox(ctx, db, agent, localinbox.ListOpts{Limit: opts.limit, Status: opts.status})
			if err != nil {
				return err
			}
			return env.printer().Print(RenderData{
				Brief:  fmt.Sprintf("%d inbox item(s)", len(rows)),
				Prompt: renderInboxRowsPrompt("Inbox: "+agent, rows),
				JSON:   rows,
			})
		},
	}
	cmd.Flags().StringVar(&opts.agent, "agent", "", "agent name (default: current profile)")
	cmd.Flags().BoolVar(&opts.global, "global", false, "show global feed instead of agent inbox")
	cmd.Flags().StringVar(&opts.status, "status", "", "status filter or all")
	cmd.Flags().IntVar(&opts.limit, "limit", 20, "result limit")
	return cmd
}

func newLatestCommand(env *CommandEnv) *cobra.Command {
	opts := &inboxCommandOptions{limit: 20}
	cmd := &cobra.Command{
		Use:   "latest",
		Short: "List latest local events",
		RunE: func(_ *cobra.Command, _ []string) error {
			ctx, cancel := env.context()
			defer cancel()
			db, closeDB, err := openInboxQueryDB(ctx)
			if err != nil {
				return err
			}
			defer closeDB()
			rows, err := localinbox.ListLatest(ctx, db, opts.limit)
			if err != nil {
				return err
			}
			return env.printer().Print(RenderData{
				Brief:  fmt.Sprintf("%d event(s)", len(rows)),
				Prompt: renderEventRowsPrompt("Latest Events", rows),
				JSON:   rows,
			})
		},
	}
	cmd.Flags().IntVar(&opts.limit, "limit", 20, "result limit")
	return cmd
}

func newThreadCommand(env *CommandEnv) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "thread <thread_id>",
		Short: "List local events in a thread",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			ctx, cancel := env.context()
			defer cancel()
			db, closeDB, err := openInboxQueryDB(ctx)
			if err != nil {
				return err
			}
			defer closeDB()
			rows, err := localinbox.ListThread(ctx, db, args[0])
			if err != nil {
				return err
			}
			return env.printer().Print(RenderData{
				Brief:  fmt.Sprintf("%d thread event(s)", len(rows)),
				Prompt: renderEventRowsPrompt("Thread: "+strings.TrimSpace(args[0]), rows),
				JSON:   rows,
			})
		},
	}
	return cmd
}

func newInboxStatusCommand(env *CommandEnv, name string) *cobra.Command {
	opts := &inboxCommandOptions{}
	cmd := &cobra.Command{
		Use:   name + " <event_id>",
		Short: "Mark local inbox event " + name,
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			ctx, cancel := env.context()
			defer cancel()
			db, closeDB, err := openInboxStatusDB(ctx)
			if err != nil {
				return err
			}
			defer closeDB()
			agent := effectiveInboxAgent(env, opts.agent)
			var result localinbox.StatusResult
			switch name {
			case "ack":
				result, err = localinbox.Ack(ctx, db, args[0], agent)
			case "done":
				result, err = localinbox.Done(ctx, db, args[0], agent)
			case "fail":
				result, err = localinbox.Fail(ctx, db, args[0], agent, opts.error)
			case "skip":
				result, err = localinbox.Skip(ctx, db, args[0], agent, opts.reason)
			default:
				err = fmt.Errorf("unsupported inbox status command %q", name)
			}
			if err != nil {
				if errors.Is(err, localinbox.ErrNotApplicable) {
					return fmt.Errorf("%w: %s for %s", localinbox.ErrNotApplicable, args[0], agent)
				}
				return err
			}
			prompt := fmt.Sprintf("# Inbox Status Updated\n\nEvent: `%s`\nAgent: `%s`\nStatus: `%s`\nRows affected: %d",
				result.EventID, result.Agent, result.Status, result.RowsAffected)
			return env.printer().Print(RenderData{
				Brief:  fmt.Sprintf("%s %s", result.Status, result.EventID),
				Prompt: prompt,
				JSON:   result,
			})
		},
	}
	cmd.Flags().StringVar(&opts.agent, "agent", "", "agent name (default: current profile)")
	cmd.Flags().StringVar(&opts.reason, "reason", "", "skip reason")
	cmd.Flags().StringVar(&opts.error, "error", "", "failure message")
	return cmd
}

func openInboxQueryDB(ctx context.Context) (*sql.DB, func() error, error) {
	exists, _, err := localstate.Exists()
	if err != nil {
		return nil, nil, err
	}
	if exists {
		db, closeDB, err := localstate.Open(ctx)
		if err != nil {
			return nil, nil, err
		}
		if err := localstate.Migrate(ctx, db); err != nil {
			_ = closeDB()
			return nil, nil, err
		}
		return db, closeDB, nil
	}
	db, closeDB, err := localstate.OpenPath(ctx, ":memory:")
	if err != nil {
		return nil, nil, err
	}
	if err := localstate.Migrate(ctx, db); err != nil {
		_ = closeDB()
		return nil, nil, err
	}
	eventsPath, err := defaultEventsNDJSONPath()
	if err != nil {
		_ = closeDB()
		return nil, nil, err
	}
	if _, err := os.Stat(eventsPath); err == nil {
		if err := localinbox.Ingest(ctx, db, eventsPath); err != nil {
			_ = closeDB()
			return nil, nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		_ = closeDB()
		return nil, nil, err
	}
	return db, closeDB, nil
}

func openInboxStatusDB(ctx context.Context) (*sql.DB, func() error, error) {
	db, closeDB, err := localstate.Open(ctx)
	if err != nil {
		return nil, nil, err
	}
	if err := localstate.Migrate(ctx, db); err != nil {
		_ = closeDB()
		return nil, nil, err
	}
	eventsPath, err := defaultEventsNDJSONPath()
	if err != nil {
		_ = closeDB()
		return nil, nil, err
	}
	if _, statErr := os.Stat(eventsPath); statErr == nil {
		if err := localinbox.Ingest(ctx, db, eventsPath); err != nil {
			_ = closeDB()
			return nil, nil, err
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		_ = closeDB()
		return nil, nil, statErr
	}
	return db, closeDB, nil
}

func effectiveInboxAgent(env *CommandEnv, explicit string) string {
	if value := strings.TrimSpace(explicit); value != "" {
		return value
	}
	if value := strings.TrimSpace(env.opts.profile); value != "" {
		return value
	}
	return DefaultProfileName
}

func defaultEventsNDJSONPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".aitask", "events.ndjson"), nil
}

func renderInboxRowsPrompt(title string, rows []localinbox.InboxRow) string {
	lines := []string{"# " + title, ""}
	if len(rows) == 0 {
		lines = append(lines, "(empty)")
		return strings.Join(lines, "\n")
	}
	for _, row := range rows {
		status := row.Status
		if status == "" {
			status = row.Visibility
		}
		lines = append(lines, fmt.Sprintf("- [%s] %s `%s` from `%s`: %s", status, row.Kind, row.ID, row.FromAgent, compactInboxBody(row.Body)))
	}
	return strings.Join(lines, "\n")
}

func renderEventRowsPrompt(title string, rows []localinbox.EventRow) string {
	lines := []string{"# " + title, ""}
	if len(rows) == 0 {
		lines = append(lines, "(empty)")
		return strings.Join(lines, "\n")
	}
	for _, row := range rows {
		lines = append(lines, fmt.Sprintf("- [%s] %s `%s`: %s", row.CreatedAt, row.Kind, row.ID, compactInboxBody(row.Body)))
	}
	return strings.Join(lines, "\n")
}

func compactInboxBody(body string) string {
	body = strings.Join(strings.Fields(body), " ")
	if body == "" {
		return "(no body)"
	}
	if len(body) > 160 {
		return body[:157] + "..."
	}
	return body
}

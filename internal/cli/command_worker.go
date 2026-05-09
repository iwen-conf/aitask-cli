package cli

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	localstate "github.com/iwen-conf/aitask-cli/internal/state"
	localworker "github.com/iwen-conf/aitask-cli/internal/worker"
	"github.com/spf13/cobra"
)

type workerCommandOptions struct {
	once          bool
	daemon        bool
	memory        string
	interval      time.Duration
	batch         int
	maxRetries    int
	quiet         bool
	backfillSince string
	backfillLimit int
	dryRun        bool
}

func newWorkerCommand(env *CommandEnv) *cobra.Command {
	opts := &workerCommandOptions{once: true, memory: "backend", interval: 10 * time.Second, batch: 50, maxRetries: 5}
	cmd := &cobra.Command{
		Use:   "worker",
		Short: "Index local events and sync semantic memory",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if opts.once && opts.daemon || strings.TrimSpace(opts.backfillSince) != "" && (opts.daemon || !opts.once) {
				return fmt.Errorf("--once, --daemon, and --backfill-since are mutually exclusive")
			}
			if opts.daemon {
				opts.once = false
			}
			ctx, cancel := workerContext(env, opts.daemon)
			defer cancel()
			db, closeDB, err := localstate.Open(ctx)
			if err != nil {
				return err
			}
			defer closeDB()
			if err := localstate.Migrate(ctx, db); err != nil {
				return err
			}
			if strings.TrimSpace(opts.backfillSince) != "" {
				since, err := time.Parse(time.RFC3339, strings.TrimSpace(opts.backfillSince))
				if err != nil {
					return fmt.Errorf("invalid --backfill-since RFC3339 timestamp: %w; hint: use 2026-05-08T00:00:00Z", err)
				}
				stats, err := localworker.BackfillMemorySync(ctx, localworker.BackfillOptions{StateDB: db, Since: since, Limit: opts.backfillLimit, DryRun: opts.dryRun, Logger: workerLogger(env, opts.quiet)})
				if err != nil {
					return err
				}
				return env.printer().Print(RenderData{
					Brief:  fmt.Sprintf("backfill matched=%d inserted=%d dryRun=%t", stats.Matched, stats.Inserted, stats.DryRun),
					Prompt: renderWorkerBackfillPrompt(stats),
					JSON:   stats,
				})
			}
			eventsPath, err := defaultEventsNDJSONPath()
			if err != nil {
				return err
			}
			syncer, err := buildWorkerSyncer(env, opts.memory)
			if err != nil {
				return err
			}
			workerOpts := localworker.Options{
				StateDB:    db,
				NDJSONPath: eventsPath,
				Sync:       syncer,
				Interval:   opts.interval,
				BatchSize:  opts.batch,
				MaxRetries: opts.maxRetries,
				Logger:     workerLogger(env, opts.quiet),
			}
			if opts.daemon {
				if err := localworker.RunDaemon(ctx, workerOpts); err != nil && !errors.Is(err, context.Canceled) {
					return err
				}
				return nil
			}
			stats, err := localworker.RunOnce(ctx, workerOpts)
			if err != nil {
				return err
			}
			return env.printer().Print(RenderData{
				Brief:  fmt.Sprintf("ingested=%d sync=%d/%d", stats.Ingested, stats.SyncSucceeded, stats.SyncFailed),
				Prompt: renderWorkerStatsPrompt(stats),
				JSON:   stats,
			})
		},
	}
	cmd.Flags().BoolVar(&opts.once, "once", true, "run one indexing tick")
	cmd.Flags().BoolVar(&opts.daemon, "daemon", false, "run continuously until interrupted")
	cmd.Flags().StringVar(&opts.memory, "memory", "backend", "memory sync mode: backend|openviking|none")
	cmd.Flags().DurationVar(&opts.interval, "interval", 10*time.Second, "daemon interval")
	cmd.Flags().IntVar(&opts.batch, "batch", 50, "memory sync batch size")
	cmd.Flags().IntVar(&opts.maxRetries, "max-retries", 5, "max sync retries before skipping")
	cmd.Flags().BoolVar(&opts.quiet, "quiet", false, "suppress worker log line")
	cmd.Flags().StringVar(&opts.backfillSince, "backfill-since", "", "backfill memory_sync rows for events created at or after RFC3339 timestamp")
	cmd.Flags().IntVar(&opts.backfillLimit, "limit", 0, "backfill event limit")
	cmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "print backfill candidates without writing memory_sync")
	return cmd
}

func buildWorkerSyncer(env *CommandEnv, mode string) (localworker.Syncer, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "backend", "openviking":
		cfg, err := env.resolveProjectConfig(true)
		if err != nil {
			return nil, err
		}
		client, _, err := env.clientWithToken(true)
		if err != nil {
			return nil, err
		}
		return &backendSyncer{client: client, projectID: cfg.ProjectID}, nil
	case "none":
		return nil, nil
	default:
		return nil, fmt.Errorf("unsupported memory mode %q", mode)
	}
}

func workerContext(env *CommandEnv, daemon bool) (context.Context, context.CancelFunc) {
	if !daemon {
		return env.context()
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	return ctx, cancel
}

func workerLogger(env *CommandEnv, quiet bool) *log.Logger {
	if quiet {
		return log.New(ioDiscard{}, "", 0)
	}
	return log.New(env.app.Stderr, "", 0)
}

func renderWorkerStatsPrompt(stats localworker.Stats) string {
	return fmt.Sprintf(`# Worker

- Ingested: %d
- Routed agent: %d
- Routed global: %d
- Sync succeeded: %d
- Sync failed: %d
- Summaries updated: %d`,
		stats.Ingested,
		stats.RoutedAgent,
		stats.RoutedGlobal,
		stats.SyncSucceeded,
		stats.SyncFailed,
		stats.SummariesUpdated)
}

func renderWorkerBackfillPrompt(stats localworker.BackfillStats) string {
	lines := []string{
		"# Worker Backfill",
		"",
		fmt.Sprintf("- Matched: %d", stats.Matched),
		fmt.Sprintf("- Inserted: %d", stats.Inserted),
		fmt.Sprintf("- Dry run: %t", stats.DryRun),
		"",
		"## Event IDs",
		"",
	}
	if len(stats.EventIDs) == 0 {
		lines = append(lines, "(empty)")
	} else {
		for _, id := range stats.EventIDs {
			lines = append(lines, "- "+id)
		}
	}
	return strings.Join(lines, "\n")
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) {
	return len(p), nil
}

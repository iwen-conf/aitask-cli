package cli

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	localstate "github.com/iwen-conf/aitask-cli/internal/state"
	localworker "github.com/iwen-conf/aitask-cli/internal/worker"
	"github.com/spf13/cobra"
)

type workerCommandOptions struct {
	once     bool
	daemon   bool
	interval time.Duration
	quiet    bool
}

func newWorkerCommand(env *CommandEnv) *cobra.Command {
	opts := &workerCommandOptions{once: true, interval: 10 * time.Second}
	cmd := &cobra.Command{
		Use:   "worker",
		Short: "Index local events into state.db",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if opts.once && opts.daemon {
				return fmt.Errorf("--once and --daemon are mutually exclusive")
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
			eventsPath, err := defaultEventsNDJSONPath()
			if err != nil {
				return err
			}
			workerOpts := localworker.Options{
				StateDB:    db,
				NDJSONPath: eventsPath,
				Interval:   opts.interval,
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
				Brief:  fmt.Sprintf("ingested=%d summaries=%d", stats.Ingested, stats.SummariesUpdated),
				Prompt: renderWorkerStatsPrompt(stats),
				JSON:   stats,
			})
		},
	}
	cmd.Flags().BoolVar(&opts.once, "once", true, "run one indexing tick")
	cmd.Flags().BoolVar(&opts.daemon, "daemon", false, "run continuously until interrupted")
	cmd.Flags().DurationVar(&opts.interval, "interval", 10*time.Second, "daemon interval")
	cmd.Flags().BoolVar(&opts.quiet, "quiet", false, "suppress worker log line")
	return cmd
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
- Summaries updated: %d`,
		stats.Ingested,
		stats.RoutedAgent,
		stats.RoutedGlobal,
		stats.SummariesUpdated)
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) {
	return len(p), nil
}

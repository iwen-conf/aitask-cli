package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	localwatch "github.com/iwen-conf/aitask-cli/internal/agentwatch"
	localstate "github.com/iwen-conf/aitask-cli/internal/state"
	"github.com/spf13/cobra"
)

type watchCommandOptions struct {
	agent      string
	once       bool
	execScript string
	wake       string
	dryRun     bool
	interval   time.Duration
	timeout    time.Duration
	maxRetries int
	quiet      bool
}

func newWatchCommand(env *CommandEnv) *cobra.Command {
	opts := &watchCommandOptions{interval: 5 * time.Second, timeout: 5 * time.Minute, maxRetries: 5}
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Consume local inbox events as an agent",
		RunE: func(cmd *cobra.Command, _ []string) error {
			agent := effectiveInboxAgent(env, opts.agent)
			runner, dryRun, err := resolveWatchRunner(env, cmd, opts, agent)
			if err != nil {
				return err
			}
			ctx, cancel := watchContext(env, !opts.once)
			defer cancel()
			db, closeDB, err := localstate.Open(ctx)
			if err != nil {
				return err
			}
			defer closeDB()
			if err := localstate.Migrate(ctx, db); err != nil {
				return err
			}
			recaller := buildBackendRecaller(env)
			var promptOut bytes.Buffer
			watchOpts := localwatch.Options{
				StateDB:         db,
				Agent:           agent,
				Runner:          runner,
				ContextRecaller: recaller,
				Once:            opts.once,
				Interval:        opts.interval,
				Timeout:         opts.timeout,
				DryRun:          dryRun,
				Logger:          watchLogger(env, opts.quiet),
				MaxRetries:      opts.maxRetries,
			}
			if dryRun {
				watchOpts.Once = true
				watchOpts.PromptWriter = &promptOut
			}
			if watchOpts.Once {
				stats, err := localwatch.RunOnce(ctx, watchOpts)
				if err != nil {
					return err
				}
				if dryRun {
					prompt := strings.TrimSpace(promptOut.String())
					return env.printer().Print(RenderData{Brief: fmt.Sprintf("picked=%d", stats.Picked), Prompt: prompt, JSON: map[string]any{"stats": stats, "prompt": prompt}})
				}
				return env.printer().Print(RenderData{Brief: fmt.Sprintf("done=%d failed=%d skipped=%d", stats.Done, stats.Failed, stats.Skipped), Prompt: renderWatchStatsPrompt(stats), JSON: stats})
			}
			if err := localwatch.RunLoop(ctx, watchOpts); err != nil && !errors.Is(err, context.Canceled) {
				return err
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.agent, "agent", "", "agent name (default: current profile)")
	cmd.Flags().BoolVar(&opts.once, "once", false, "run one drain tick and exit")
	cmd.Flags().StringVar(&opts.execScript, "exec", "", "handler executable that reads prompt from stdin")
	cmd.Flags().StringVar(&opts.wake, "wake", "", "one-shot runner: claude|codex|gemini")
	cmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "render prompt only; do not call runner or change status")
	cmd.Flags().DurationVar(&opts.interval, "interval", 5*time.Second, "watch interval")
	cmd.Flags().DurationVar(&opts.timeout, "timeout", 5*time.Minute, "per-event runner timeout")
	cmd.Flags().IntVar(&opts.maxRetries, "max-retries", 5, "max runner retries before skipping")
	cmd.Flags().BoolVar(&opts.quiet, "quiet", false, "suppress watcher logs")
	cmd.Flags().Lookup("wake").NoOptDefVal = "auto"
	return cmd
}

func resolveWatchRunner(env *CommandEnv, cmd *cobra.Command, opts *watchCommandOptions, agent string) (localwatch.Runner, bool, error) {
	execSet := strings.TrimSpace(opts.execScript) != ""
	wakeSet := cmd.Flags().Changed("wake")
	dryRun := opts.dryRun
	selected := 0
	if execSet {
		selected++
	}
	if wakeSet {
		selected++
	}
	if dryRun {
		selected++
	}
	if selected == 0 {
		if isInteractiveWriter(env.app.Stdout) {
			return nil, true, nil
		}
		return nil, false, fmt.Errorf("one of --exec, --wake, or --dry-run is required when stdout is not a TTY")
	}
	if selected > 1 {
		return nil, false, fmt.Errorf("--exec, --wake, and --dry-run are mutually exclusive")
	}
	if dryRun {
		return nil, true, nil
	}
	if execSet {
		runner, err := newExecRunner(opts.execScript)
		return runner, false, err
	}
	runner, err := newWakeRunner(opts.wake, agent)
	return runner, false, err
}

type commandRunner struct {
	name      string
	args      []string
	stdin     bool
	promptArg bool
}

func newExecRunner(script string) (localwatch.Runner, error) {
	script = strings.TrimSpace(script)
	if script == "" {
		return nil, fmt.Errorf("--exec cannot be empty")
	}
	resolved := script
	if !strings.ContainsAny(script, `/\`) {
		path, err := exec.LookPath(script)
		if err != nil {
			return nil, fmt.Errorf("handler %q not found on PATH", script)
		}
		resolved = path
	} else if info, err := os.Stat(script); err != nil {
		return nil, err
	} else if info.IsDir() {
		return nil, fmt.Errorf("handler %q is a directory", script)
	}
	return &commandRunner{name: resolved, stdin: true}, nil
}

func newWakeRunner(value, agent string) (localwatch.Runner, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || value == "auto" {
		value = defaultWakeForAgent(agent)
	}
	switch value {
	case "claude", "claude-code":
		if _, err := exec.LookPath("claude"); err != nil {
			return nil, fmt.Errorf("claude binary not found on PATH")
		}
		return &commandRunner{name: "claude", args: []string{"-p"}, promptArg: true}, nil
	case "codex":
		if _, err := exec.LookPath("codex"); err != nil {
			return nil, fmt.Errorf("codex binary not found on PATH")
		}
		return &commandRunner{name: "codex", args: []string{"exec"}, stdin: true}, nil
	case "gemini":
		if _, err := exec.LookPath("gemini"); err != nil {
			return nil, fmt.Errorf("gemini binary not found on PATH")
		}
		return &commandRunner{name: "gemini", stdin: true}, nil
	default:
		return nil, fmt.Errorf("unsupported --wake value %q", value)
	}
}

func defaultWakeForAgent(agent string) string {
	agent = strings.ToLower(strings.TrimSpace(agent))
	switch {
	case strings.Contains(agent, "claude"):
		return "claude"
	case strings.Contains(agent, "codex"):
		return "codex"
	case strings.Contains(agent, "gemini"):
		return "gemini"
	default:
		return ""
	}
}

func (r *commandRunner) Run(ctx context.Context, prompt string) (localwatch.RunResult, error) {
	args := append([]string{}, r.args...)
	if r.promptArg {
		args = append(args, prompt)
	}
	cmd := exec.CommandContext(ctx, r.name, args...)
	if r.stdin {
		cmd.Stdin = strings.NewReader(prompt)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := localwatch.RunResult{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: 0}
	if err == nil {
		return result, nil
	}
	if exitErr := new(exec.ExitError); errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
		return result, nil
	}
	result.ExitCode = -1
	return result, err
}

func watchContext(env *CommandEnv, daemon bool) (context.Context, context.CancelFunc) {
	if !daemon {
		return env.context()
	}
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

func watchLogger(env *CommandEnv, quiet bool) *log.Logger {
	if quiet {
		return log.New(ioDiscard{}, "", 0)
	}
	return log.New(env.app.Stderr, "", 0)
}

func renderWatchStatsPrompt(stats localwatch.Stats) string {
	return fmt.Sprintf(`# Agent Watch

- Picked: %d
- Acked: %d
- Done: %d
- Failed: %d
- Skipped: %d`, stats.Picked, stats.Acked, stats.Done, stats.Failed, stats.Skipped)
}

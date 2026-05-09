package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
)

// SpecializedBuilder produces the cobra subcommand whose flags + RunE will be
// promoted to the root of a specialized binary (aitask-watch, aitask-worker,
// aitask-agent-watch). It is given the fully wired CommandEnv that the
// specialized binary shares with the umbrella aitask CLI.
type SpecializedBuilder func(env *CommandEnv) *cobra.Command

// NewSpecializedRootCommand wires the same persistent flags / pre-run hooks as
// NewRootCommand but exposes only one subcommand at the root, named after the
// binary itself. It is what each `cli/aitask-*` main.go calls.
func (a *App) NewSpecializedRootCommand(name, short string, build SpecializedBuilder) (*cobra.Command, error) {
	if a.Stdout == nil {
		a.Stdout = os.Stdout
	}
	if a.Stderr == nil {
		a.Stderr = os.Stderr
	}
	if a.Stdin == nil {
		a.Stdin = os.Stdin
	}

	tokenStore, err := NewTokenStore()
	if err != nil {
		return nil, err
	}
	opts := &globalOptions{serverURL: defaultServerURL, formatRaw: string(FormatPrompt), timeout: 15 * time.Second}
	env := &CommandEnv{app: a, opts: opts, tokenStore: tokenStore}

	inner := build(env)

	root := &cobra.Command{
		Use:     name,
		Short:   short,
		Version: versionOrDev(a.Version),
		Args:    inner.Args,
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
			format, err := ParseOutputFormat(opts.formatRaw)
			if err != nil {
				return err
			}
			opts.format = format
			opts.serverURL = normalizeServerURL(opts.serverURL)
			opts.profile, err = resolveEffectiveProfile(opts.profileRaw)
			return err
		},
		RunE: inner.RunE,
	}
	root.SetVersionTemplate(fmt.Sprintf("%s version %s\n", name, versionOrDev(a.Version)))

	root.SilenceUsage = true
	root.SilenceErrors = true
	root.SetOut(a.Stdout)
	root.SetErr(a.Stderr)

	root.PersistentFlags().StringVar(&opts.serverURL, "server", resolveDefaultServerURL(), "AITask backend base URL")
	root.PersistentFlags().StringVar(&opts.formatRaw, "format", string(FormatPrompt), "output format: brief|prompt|json|proto")
	root.PersistentFlags().DurationVar(&opts.timeout, "timeout", 15*time.Second, "request timeout")
	root.PersistentFlags().StringVar(&opts.projectID, "project", "", "override project_id for project-scoped commands")
	root.PersistentFlags().StringVar(&opts.profileRaw, "profile", "", "agent identity profile (default: $AITASK_PROFILE > config.active_profile > \"default\")")

	root.Flags().AddFlagSet(inner.Flags())

	for _, sub := range inner.Commands() {
		root.AddCommand(sub)
	}

	return root, nil
}

// ExecuteSpecialized is the convenience wrapper that the specialized binaries
// call from their main.go. It mirrors App.Execute but uses NewSpecializedRootCommand.
func (a *App) ExecuteSpecialized(name, short string, build SpecializedBuilder, args []string) error {
	cmd, err := a.NewSpecializedRootCommand(name, short, build)
	if err != nil {
		return err
	}
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		return enhanceCommandError(err)
	}
	return nil
}

// NewWatchSubcommand exposes the events streamer for aitask-watch's main.go.
// It is the legacy "events.ndjson subscriber" daemon, originally registered as
// `aitask events`.
func NewWatchSubcommand(env *CommandEnv) *cobra.Command { return newEventsDaemonCommand(env) }

// NewWorkerSubcommand exposes the local indexer + memory sync worker for
// aitask-worker's main.go.
func NewWorkerSubcommand(env *CommandEnv) *cobra.Command { return newWorkerCommand(env) }

// NewAgentWatchSubcommand exposes the per-agent inbox consumer for
// aitask-agent-watch's main.go. It is the same logic as `aitask watch`.
func NewAgentWatchSubcommand(env *CommandEnv) *cobra.Command { return newWatchCommand(env) }

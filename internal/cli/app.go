package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type App struct {
	Version string
	Stdout  io.Writer
	Stderr  io.Writer
	Stdin   io.Reader
}

type globalOptions struct {
	serverURL  string
	formatRaw  string
	format     OutputFormat
	timeout    time.Duration
	projectID  string
	profileRaw string // exact value passed via --profile (may be empty when unset)
	profile    string // resolved profile name after PersistentPreRunE
}

type CommandEnv struct {
	app        *App
	opts       *globalOptions
	tokenStore *TokenStore
}

func NewApp(version string) *App {
	return &App{Version: version, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin}
}

func (a *App) Execute(args []string) error {
	cmd, err := a.NewRootCommand()
	if err != nil {
		return err
	}
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		return enhanceCommandError(err)
	}
	return nil
}

func (a *App) NewRootCommand() (*cobra.Command, error) {
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

	root := &cobra.Command{
		Use:     "aitask",
		Short:   "AITask CLI",
		Long:    "AITask CLI for project bootstrap, delegated tasks, memory, skills, and room collaboration.",
		Version: versionOrDev(a.Version),
		Args:    cobra.NoArgs,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			format, err := ParseOutputFormat(opts.formatRaw)
			if err != nil {
				return err
			}
			opts.format = format
			opts.serverURL = normalizeServerURL(opts.serverURL)
			opts.profile, err = resolveEffectiveProfile(opts.profileRaw)
			if err != nil {
				return err
			}
			maybeStartWatchDaemon(cmd)
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			// No subcommand: launch TUI on a TTY, otherwise show help.
			if isInteractiveStdin(a.Stdin) && isInteractiveWriter(a.Stdout) {
				return RunTUI(env)
			}
			return cmd.Help()
		},
	}
	root.SetVersionTemplate(fmt.Sprintf("aitask version %s\n", versionOrDev(a.Version)))

	root.SilenceUsage = true
	root.SilenceErrors = true
	root.SetOut(a.Stdout)
	root.SetErr(a.Stderr)

	root.PersistentFlags().StringVar(&opts.serverURL, "server", resolveDefaultServerURL(), "AITask backend base URL")
	root.PersistentFlags().StringVar(&opts.formatRaw, "format", string(FormatPrompt), "output format: brief|prompt|json|proto")
	root.PersistentFlags().DurationVar(&opts.timeout, "timeout", 15*time.Second, "request timeout")
	root.PersistentFlags().StringVar(&opts.projectID, "project", "", "override project_id for project-scoped commands")
	root.PersistentFlags().StringVar(&opts.profileRaw, "profile", "", "agent identity profile (default: $AITASK_PROFILE > config.active_profile > \"default\")")

	root.AddCommand(
		newVersionCommand(env),
		newAuthCommand(env),
		newWhoAmICommand(env),
		newInitCommand(env),
		newProjectCommand(env),
		newBootstrapCommand(env),
		newContextCommand(env),
		newRunCommand(env),
		newSearchCommand(env),
		newSummaryCommand(env),
		newTaskCommand(env),
		newMemoryCommand(env),
		newOpenVikingCommand(env),
		newSkillCommand(env),
		newEventsCommand(env),
		newWorkerCommand(env),
		newWatchCommand(env),
		newRenderPromptCommand(env),
		newInboxCommand(env),
		newLatestCommand(env),
		newThreadCommand(env),
		newInboxStatusCommand(env, "ack"),
		newInboxStatusCommand(env, "done"),
		newInboxStatusCommand(env, "fail"),
		newInboxStatusCommand(env, "skip"),
		newRoomCommand(env),
	)

	return root, nil
}

func (e *CommandEnv) printer() *Printer {
	return NewPrinter(e.opts.format, e.app.Stdout)
}

func (e *CommandEnv) context() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), e.opts.timeout)
}

func (e *CommandEnv) clientWithToken(required bool) (*Client, string, error) {
	token, err := e.tokenStore.Load(e.opts.serverURL, e.opts.profile)
	if err != nil {
		if required {
			return nil, "", err
		}
		return NewClient(e.opts.serverURL, e.opts.timeout, ""), "", nil
	}
	return NewClient(e.opts.serverURL, e.opts.timeout, token), token, nil
}

func (e *CommandEnv) clientWithoutToken() *Client {
	return NewClient(e.opts.serverURL, e.opts.timeout, "")
}

func (e *CommandEnv) resolveProjectConfig(requireFile bool) (ProjectConfig, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return ProjectConfig{}, err
	}
	if strings.TrimSpace(e.opts.projectID) != "" {
		if cfg, loadErr := LoadProjectConfig(cwd); loadErr == nil {
			cfg.ProjectID = strings.TrimSpace(e.opts.projectID)
			if cfg.OpenVikingRoot == "" {
				cfg.OpenVikingRoot = "viking://aitask/projects/" + cfg.ProjectID
			}
			return cfg, nil
		}
		aiDir := filepath.Join(cwd, AITaskDirName)
		return ProjectConfig{
			RootDir:        cwd,
			AITaskDir:      aiDir,
			SourceFile:     filepath.Join(aiDir, "project.md"),
			ProjectID:      strings.TrimSpace(e.opts.projectID),
			OpenVikingRoot: "viking://aitask/projects/" + strings.TrimSpace(e.opts.projectID),
			RoomEnabled:    true,
		}, nil
	}
	cfg, err := LoadProjectConfig(cwd)
	if err != nil {
		if requireFile {
			return ProjectConfig{}, err
		}
		return ProjectConfig{}, nil
	}
	return cfg, nil
}

func normalizeServerURL(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return defaultServerURL
	}
	value = strings.TrimRight(value, "/")
	if !strings.Contains(value, "://") {
		value = "http://" + value
	}
	return value
}

func getenvDefault(key string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func versionOrDev(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		return "dev"
	}
	return version
}

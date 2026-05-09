package cli

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type bindCodeEnvelope struct {
	AgentID string `json:"agentId"`
	Token   string `json:"token"`
}

func newAuthCommand(env *CommandEnv) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage local agent token",
	}

	bind := &cobra.Command{
		Use:   "bind",
		Short: "Bind token from one-time code",
		RunE: func(cmd *cobra.Command, _ []string) error {
			code, _ := cmd.Flags().GetString("code")
			profile, _ := cmd.Flags().GetString("profile")
			token, err := parseBindCode(code)
			if err != nil {
				return err
			}
			return importAndVerifyToken(env, token, profile)
		},
	}
	bind.Flags().String("code", "", "one-time binding code")
	bind.Flags().String("profile", "", "profile to bind into (default: active profile)")
	_ = bind.MarkFlagRequired("code")

	var inlineToken string
	importCmd := &cobra.Command{
		Use:   "import",
		Short: "Import token manually",
		RunE: func(cmd *cobra.Command, _ []string) error {
			profile, _ := cmd.Flags().GetString("profile")
			token := strings.TrimSpace(inlineToken)
			if token == "" {
				fmt.Fprint(env.app.Stderr, "Paste agent token: ")
				reader := bufio.NewReader(env.app.Stdin)
				raw, err := reader.ReadString('\n')
				if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, os.ErrClosed) {
					return err
				}
				token = strings.TrimSpace(raw)
			}
			return importAndVerifyToken(env, token, profile)
		},
	}
	importCmd.Flags().StringVar(&inlineToken, "token", "", "agent token")
	importCmd.Flags().String("profile", "", "profile to import into (default: active profile)")

	tokenCmd := &cobra.Command{Use: "token", Short: "Token commands"}
	tokenCmd.AddCommand(importCmd)

	cmd.AddCommand(bind, tokenCmd, newAuthProfileCommand(env))
	return cmd
}

// importAndVerifyToken validates a token against the backend and persists it
// into the chosen profile slot. The flag override is preferred over the
// session-resolved profile so `aitask auth bind --profile codex` works even
// when AITASK_PROFILE points elsewhere.
func importAndVerifyToken(env *CommandEnv, token string, profileFlag string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return errors.New("token cannot be empty")
	}
	profile, err := pickProfile(env, profileFlag)
	if err != nil {
		return err
	}
	client := NewClient(env.opts.serverURL, env.opts.timeout, token)
	ctx, cancel := env.context()
	defer cancel()
	whoami, err := client.WhoAmI(ctx)
	if err != nil {
		return err
	}
	if err := env.tokenStore.Save(env.opts.serverURL, profile, token); err != nil {
		return err
	}
	identity := whoami.GetIdentity()
	if err := upsertProfileRecord(env.opts.serverURL, profile, identity.GetAgentId(), identity.GetAgentType(), identity.GetRole()); err != nil {
		// Registry write failure is non-fatal — the token store is the
		// source of truth, and `auth profile list` will simply show
		// "(unknown)" for fields it can't read.
		fmt.Fprintf(env.app.Stderr, "warning: profile registry not updated: %v\n", err)
	}
	result := map[string]any{
		"stored":  true,
		"profile": profile,
		"identity": map[string]any{
			"agentId":         identity.GetAgentId(),
			"agentType":       identity.GetAgentType(),
			"role":            identity.GetRole(),
			"scopes":          identity.GetScopes(),
			"allowedProjects": identity.GetAllowedProjects(),
		},
	}
	prompt := fmt.Sprintf("# Auth Bound\n\nProfile: `%s`\nAgent: `%s` (%s)\nRole: `%s`\n\nToken stored in secure local storage.\nUse it via `aitask --profile %s ...` or `aitask auth profile use %s`.",
		profile, identity.GetAgentId(), identity.GetAgentType(), identity.GetRole(), profile, profile)
	return env.printer().Print(RenderData{Brief: "Token stored: " + profile, Prompt: prompt, JSON: result, Proto: whoami})
}

// pickProfile picks the profile name an auth-mutating command should write to:
// explicit --profile flag wins; otherwise we use the env-resolved active profile.
func pickProfile(env *CommandEnv, flagValue string) (string, error) {
	if strings.TrimSpace(flagValue) != "" {
		return ValidateProfileName(flagValue)
	}
	if env.opts.profile != "" {
		return env.opts.profile, nil
	}
	return DefaultProfileName, nil
}

// upsertProfileRecord refreshes the cached identity hint for a profile in
// ~/.aitask/config.json. It does NOT touch active_profile — `use` is a
// separate explicit operation.
func upsertProfileRecord(serverURL, profile, agentID, agentType, role string) error {
	cfg, err := LoadGlobalConfig()
	if err != nil {
		return err
	}
	if cfg.Profiles == nil {
		cfg.Profiles = map[string]ProfileRecord{}
	}
	cfg.Profiles[profile] = ProfileRecord{
		AgentID:   agentID,
		AgentType: agentType,
		Role:      role,
		ServerURL: serverURL,
		StoredAt:  time.Now().UTC().Format(time.RFC3339),
	}
	return SaveGlobalConfig(cfg)
}

func newAuthProfileCommand(env *CommandEnv) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profile",
		Short: "Manage local identity profiles (multi-token)",
		Long:  "Each profile stores one agent token. Switch with `aitask auth profile use <name>`, override per-command with `--profile <name>`, or pin per shell with `export AITASK_PROFILE=<name>`.",
	}
	cmd.AddCommand(
		newAuthProfileListCommand(env),
		newAuthProfileCurrentCommand(env),
		newAuthProfileUseCommand(env),
		newAuthProfileAddCommand(env),
		newAuthProfileRemoveCommand(env),
	)
	return cmd
}

func newAuthProfileListCommand(env *CommandEnv) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List stored profiles",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := LoadGlobalConfig()
			if err != nil {
				return err
			}
			active := env.opts.profile
			if active == "" {
				active = resolveDefaultProfile()
			}
			// Self-heal: if the active profile has a usable token but no
			// registry entry (e.g. silently migrated from a pre-profile install),
			// query the backend once and persist the identity hints so subsequent
			// `list` calls are noiseless.
			if _, ok := cfg.Profiles[active]; !ok {
				if token, err := env.tokenStore.Load(env.opts.serverURL, active); err == nil {
					ctx, cancel := env.context()
					if who, werr := NewClient(env.opts.serverURL, env.opts.timeout, token).WhoAmI(ctx); werr == nil {
						id := who.GetIdentity()
						_ = upsertProfileRecord(env.opts.serverURL, active, id.GetAgentId(), id.GetAgentType(), id.GetRole())
						cfg, _ = LoadGlobalConfig()
					}
					cancel()
				}
			}
			names := cfg.SortedProfileNames()
			items := make([]map[string]any, 0, len(names))
			var lines []string
			lines = append(lines, "# Profiles")
			lines = append(lines, "")
			if len(names) == 0 {
				lines = append(lines, "(empty) — bind one with `aitask auth bind --code <code> --profile <name>`")
			}
			for _, name := range names {
				rec := cfg.Profiles[name]
				marker := "  "
				if name == active {
					marker = "* "
				}
				agentLabel := strings.TrimSpace(rec.AgentType)
				if agentLabel == "" {
					agentLabel = "(unknown)"
				}
				lines = append(lines, fmt.Sprintf("- %s`%s` — %s · %s · stored %s", marker, name, agentLabel, displayOrDash(rec.AgentID), displayOrDash(rec.StoredAt)))
				items = append(items, map[string]any{
					"name":      name,
					"active":    name == active,
					"agentId":   rec.AgentID,
					"agentType": rec.AgentType,
					"role":      rec.Role,
					"serverUrl": rec.ServerURL,
					"storedAt":  rec.StoredAt,
				})
			}
			brief := fmt.Sprintf("%d profile(s), active=%s", len(names), active)
			return env.printer().Print(RenderData{
				Brief:  brief,
				Prompt: strings.Join(lines, "\n"),
				JSON: map[string]any{
					"active":   active,
					"profiles": items,
				},
			})
		},
	}
}

func newAuthProfileCurrentCommand(env *CommandEnv) *cobra.Command {
	return &cobra.Command{
		Use:   "current",
		Short: "Show the currently active profile name",
		RunE: func(_ *cobra.Command, _ []string) error {
			active := env.opts.profile
			if active == "" {
				active = resolveDefaultProfile()
			}
			return env.printer().Print(RenderData{
				Brief:  active,
				Prompt: fmt.Sprintf("# Active Profile\n\n`%s`", active),
				JSON:   map[string]any{"active": active},
			})
		},
	}
}

func newAuthProfileUseCommand(env *CommandEnv) *cobra.Command {
	return &cobra.Command{
		Use:   "use <name>",
		Short: "Set the active profile (persisted in ~/.aitask/config.json)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name, err := ValidateProfileName(args[0])
			if err != nil {
				return err
			}
			cfg, err := LoadGlobalConfig()
			if err != nil {
				return err
			}
			if _, ok := cfg.Profiles[name]; !ok {
				return fmt.Errorf("profile %q not found — run `aitask auth bind --code <code> --profile %s` first", name, name)
			}
			cfg.ActiveProfile = name
			if err := SaveGlobalConfig(cfg); err != nil {
				return err
			}
			return env.printer().Print(RenderData{
				Brief:  "active=" + name,
				Prompt: fmt.Sprintf("# Profile Switched\n\nActive profile is now `%s`.", name),
				JSON:   map[string]any{"active": name},
			})
		},
	}
}

func newAuthProfileAddCommand(env *CommandEnv) *cobra.Command {
	var inlineToken string
	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Add a new profile by importing a token (does not switch active profile)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name, err := ValidateProfileName(args[0])
			if err != nil {
				return err
			}
			token := strings.TrimSpace(inlineToken)
			if token == "" {
				fmt.Fprintf(env.app.Stderr, "Paste agent token for profile %q: ", name)
				reader := bufio.NewReader(env.app.Stdin)
				raw, err := reader.ReadString('\n')
				if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, os.ErrClosed) {
					return err
				}
				token = strings.TrimSpace(raw)
			}
			return importAndVerifyToken(env, token, name)
		},
	}
	cmd.Flags().StringVar(&inlineToken, "token", "", "agent token")
	return cmd
}

func newAuthProfileRemoveCommand(env *CommandEnv) *cobra.Command {
	return &cobra.Command{
		Use:     "remove <name>",
		Aliases: []string{"rm"},
		Short:   "Remove a profile (deletes its token from keychain/credentials)",
		Args:    cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name, err := ValidateProfileName(args[0])
			if err != nil {
				return err
			}
			if err := env.tokenStore.Delete(env.opts.serverURL, name); err != nil {
				return err
			}
			cfg, err := LoadGlobalConfig()
			if err != nil {
				return err
			}
			delete(cfg.Profiles, name)
			if cfg.ActiveProfile == name {
				cfg.ActiveProfile = ""
			}
			if err := SaveGlobalConfig(cfg); err != nil {
				return err
			}
			return env.printer().Print(RenderData{
				Brief:  "removed=" + name,
				Prompt: fmt.Sprintf("# Profile Removed\n\nProfile `%s` and its token have been deleted.", name),
				JSON:   map[string]any{"removed": name},
			})
		},
	}
}

func displayOrDash(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "—"
	}
	return v
}

func parseBindCode(code string) (string, error) {
	trimmed := strings.TrimSpace(code)
	if trimmed == "" {
		return "", errors.New("code cannot be empty")
	}
	if strings.HasPrefix(trimmed, "aitask-bind:") {
		payload := strings.TrimPrefix(trimmed, "aitask-bind:")
		raw, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			raw, err = base64.RawStdEncoding.DecodeString(payload)
			if err != nil {
				return "", fmt.Errorf("invalid bind code payload: %w", err)
			}
		}
		var envelope bindCodeEnvelope
		if err := json.Unmarshal(raw, &envelope); err != nil {
			return "", err
		}
		if strings.TrimSpace(envelope.Token) == "" {
			return "", errors.New("bind code token is empty")
		}
		return strings.TrimSpace(envelope.Token), nil
	}
	if strings.Contains(trimmed, ":") {
		parts := strings.SplitN(trimmed, ":", 2)
		if strings.HasPrefix(parts[0], "agt_") && strings.TrimSpace(parts[1]) != "" {
			return strings.TrimSpace(parts[1]), nil
		}
	}
	return trimmed, nil
}

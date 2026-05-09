package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	ovconf "github.com/iwen-conf/aitask-cli/internal/openviking/conf"
)

func newOpenVikingCommand(env *CommandEnv) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "openviking",
		Short: "OpenViking CLI config integration",
		Long:  "Inspect and import the standard ~/.openviking/ovcli.conf so AITask can reuse the same connection settings as the upstream OpenViking CLI.",
	}

	configCmd := &cobra.Command{Use: "config", Short: "OpenViking ovcli.conf operations"}

	configCmd.AddCommand(newOpenVikingConfigShowCommand(env))
	configCmd.AddCommand(newOpenVikingConfigImportCommand(env))

	cmd.AddCommand(configCmd)
	return cmd
}

func newOpenVikingConfigShowCommand(env *CommandEnv) *cobra.Command {
	var pathFlag string
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show the OpenViking CLI config that AITask would load",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, source, err := loadOpenVikingConf(pathFlag)
			if err != nil {
				if errors.Is(err, ovconf.ErrNotFound) {
					return fmt.Errorf("no ovcli.conf at %s (set %s or create %s)", source, ovconf.EnvConfigPath, ovconf.DefaultRelativePath)
				}
				return err
			}
			payload := openVikingConfPayload(cfg)
			return env.printer().Print(RenderData{
				Brief:  fmt.Sprintf("loaded %s", cfg.Source),
				Prompt: renderOpenVikingConfPrompt(cfg),
				JSON:   payload,
			})
		},
	}
	cmd.Flags().StringVar(&pathFlag, "path", "", "explicit ovcli.conf path (default: $OPENVIKING_CLI_CONFIG_FILE or ~/"+ovconf.DefaultRelativePath+")")
	return cmd
}

func newOpenVikingConfigImportCommand(env *CommandEnv) *cobra.Command {
	var pathFlag string
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Push ovcli.conf into the project's OpenViking settings on the backend",
		Long:  "Reads ~/.openviking/ovcli.conf (or $OPENVIKING_CLI_CONFIG_FILE) and PUTs it to the project's OpenViking settings. Use --dry-run to preview the request body without contacting the backend.",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, source, err := loadOpenVikingConf(pathFlag)
			if err != nil {
				if errors.Is(err, ovconf.ErrNotFound) {
					return fmt.Errorf("no ovcli.conf at %s (set %s or create %s)", source, ovconf.EnvConfigPath, ovconf.DefaultRelativePath)
				}
				return err
			}
			if !cfg.HasCredentials() {
				return fmt.Errorf("ovcli.conf at %s is missing url or api_key", cfg.Source)
			}

			body := openVikingImportBody(cfg)

			if dryRun {
				return env.printer().Print(RenderData{
					Brief:  fmt.Sprintf("dry-run: would PUT %s -> %s", cfg.Source, openVikingSettingsPath(cfg, env)),
					Prompt: renderOpenVikingImportPreview(cfg, body),
					JSON: map[string]any{
						"dryRun": true,
						"source": cfg.Source,
						"body":   redactImportBody(body),
					},
				})
			}

			projectCfg, err := env.resolveProjectConfig(true)
			if err != nil {
				return err
			}
			client, _, err := env.clientWithToken(true)
			if err != nil {
				return err
			}
			ctx, cancel := env.context()
			defer cancel()

			payload, err := client.PutREST(ctx, "/api/projects/"+projectCfg.ProjectID+"/openviking/settings", body)
			if err != nil {
				return err
			}
			return env.printer().Print(RenderData{
				Brief:  fmt.Sprintf("imported %s into project %s", cfg.Source, projectCfg.ProjectID),
				Prompt: renderOpenVikingImportResult(cfg, projectCfg.ProjectID, payload),
				JSON:   payload,
			})
		},
	}
	cmd.Flags().StringVar(&pathFlag, "path", "", "explicit ovcli.conf path (default: $OPENVIKING_CLI_CONFIG_FILE or ~/"+ovconf.DefaultRelativePath+")")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the resolved request body without calling the backend")
	return cmd
}

func loadOpenVikingConf(explicit string) (ovconf.Config, string, error) {
	if explicit = strings.TrimSpace(explicit); explicit != "" {
		cfg, err := ovconf.LoadFromPath(explicit)
		return cfg, explicit, err
	}
	defaultPath, pathErr := ovconf.DefaultPath()
	if pathErr != nil {
		return ovconf.Config{}, defaultPath, pathErr
	}
	cfg, err := ovconf.LoadFromPath(defaultPath)
	return cfg, defaultPath, err
}

func openVikingConfPayload(cfg ovconf.Config) map[string]any {
	return map[string]any{
		"source":      cfg.Source,
		"url":         cfg.URL,
		"apiKeySet":   cfg.APIKey != "",
		"apiKeyMask":  ovconf.MaskedAPIKey(cfg.APIKey),
		"rootKeySet":  cfg.RootAPIKey != "",
		"rootKeyMask": ovconf.MaskedAPIKey(cfg.RootAPIKey),
		"namespace":   cfg.Namespace,
		"workspace":   cfg.EffectiveWorkspace(),
		"account":     cfg.Account,
		"projectId":   cfg.ProjectID,
	}
}

func openVikingImportBody(cfg ovconf.Config) map[string]any {
	body := map[string]any{
		"serverUrl":         cfg.URL,
		"namespace":         cfg.Namespace,
		"workspaceId":       cfg.EffectiveWorkspace(),
		"enableMemoryWrite": true,
		"enableAutoSync":    true,
	}
	if cfg.APIKey != "" {
		body["apiKey"] = cfg.APIKey
	}
	return body
}

func redactImportBody(body map[string]any) map[string]any {
	out := make(map[string]any, len(body))
	for k, v := range body {
		if k == "apiKey" {
			if s, ok := v.(string); ok {
				out[k] = ovconf.MaskedAPIKey(s)
				continue
			}
		}
		out[k] = v
	}
	return out
}

func openVikingSettingsPath(_ ovconf.Config, env *CommandEnv) string {
	if env != nil && env.opts != nil && strings.TrimSpace(env.opts.projectID) != "" {
		return "/api/projects/" + env.opts.projectID + "/openviking/settings"
	}
	return "/api/projects/<projectId>/openviking/settings"
}

func renderOpenVikingConfPrompt(cfg ovconf.Config) string {
	var sb strings.Builder
	sb.WriteString("OpenViking CLI Config\n")
	sb.WriteString("Source: " + cfg.Source + "\n")
	sb.WriteString("URL:    " + cfg.URL + "\n")
	sb.WriteString("APIKey: " + ovconf.MaskedAPIKey(cfg.APIKey) + "\n")
	if cfg.RootAPIKey != "" {
		sb.WriteString("RootKey:" + ovconf.MaskedAPIKey(cfg.RootAPIKey) + "\n")
	}
	if v := cfg.EffectiveWorkspace(); v != "" {
		sb.WriteString("Workspace: " + v + "\n")
	}
	if cfg.Namespace != "" {
		sb.WriteString("Namespace: " + cfg.Namespace + "\n")
	}
	if cfg.Account != "" {
		sb.WriteString("Account:   " + cfg.Account + "\n")
	}
	if cfg.ProjectID != "" {
		sb.WriteString("Project:   " + cfg.ProjectID + "\n")
	}
	return sb.String()
}

func renderOpenVikingImportPreview(cfg ovconf.Config, body map[string]any) string {
	var sb strings.Builder
	sb.WriteString("Dry-run: not contacting backend.\n")
	sb.WriteString("Source: " + cfg.Source + "\n")
	sb.WriteString("Resolved body:\n")
	for _, k := range []string{"serverUrl", "namespace", "workspaceId", "enableMemoryWrite", "enableAutoSync"} {
		if v, ok := body[k]; ok {
			sb.WriteString(fmt.Sprintf("  %s = %v\n", k, v))
		}
	}
	if _, ok := body["apiKey"]; ok {
		sb.WriteString("  apiKey = " + ovconf.MaskedAPIKey(cfg.APIKey) + "\n")
	}
	return sb.String()
}

func renderOpenVikingImportResult(cfg ovconf.Config, projectID string, payload map[string]any) string {
	var sb strings.Builder
	sb.WriteString("Imported OpenViking settings.\n")
	sb.WriteString("Project: " + projectID + "\n")
	sb.WriteString("Source:  " + cfg.Source + "\n")
	if v, ok := payload["serverUrl"].(string); ok && v != "" {
		sb.WriteString("URL:     " + v + "\n")
	}
	return sb.String()
}

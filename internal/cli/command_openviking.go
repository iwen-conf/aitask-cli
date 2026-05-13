package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	ovconf "github.com/iwen-conf/aitask-cli/internal/openviking/conf"
)

func newOpenVikingCommand(env *CommandEnv) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "openviking",
		Short: "OpenViking configuration",
		Long:  "Manage the layered OpenViking configuration: global credentials (~/.aitask/config.json) and per-project namespace/workspace (.aitask/project.md).",
	}

	configCmd := &cobra.Command{Use: "config", Short: "Global OpenViking credentials (server URL + API key)"}
	configCmd.AddCommand(newOpenVikingConfigShowCommand(env))
	configCmd.AddCommand(newOpenVikingConfigSetCommand(env))
	configCmd.AddCommand(newOpenVikingConfigImportCommand(env))

	projectCmd := &cobra.Command{Use: "project", Short: "Per-project OpenViking namespace and workspace"}
	projectCmd.AddCommand(newOpenVikingProjectSetCommand(env))

	cmd.AddCommand(configCmd, projectCmd)
	return cmd
}

// --- show ---

func newOpenVikingConfigShowCommand(env *CommandEnv) *cobra.Command {
	var remote bool
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show the resolved OpenViking configuration (global + current project)",
		RunE: func(_ *cobra.Command, _ []string) error {
			global, err := LoadGlobalConfig()
			if err != nil {
				return err
			}
			projectCfg, projectErr := env.resolveProjectConfig(false)
			merged := mergedOpenVikingPayload(global, projectCfg, projectErr == nil)

			if remote {
				client, _, err := env.clientWithToken(true)
				if err != nil {
					return err
				}
				ctx, cancel := env.context()
				defer cancel()
				payload, rerr := client.GetREST(ctx, "/api/system/openviking/settings", nil)
				if rerr != nil {
					merged["remoteError"] = rerr.Error()
				} else {
					merged["remote"] = payload
				}
			}

			return env.printer().Print(RenderData{
				Brief:  briefOpenVikingMerged(merged),
				Prompt: renderOpenVikingShow(merged),
				JSON:   merged,
			})
		},
	}
	cmd.Flags().BoolVar(&remote, "remote", false, "also fetch system settings from the backend for reconciliation")
	return cmd
}

// --- config set ---

func newOpenVikingConfigSetCommand(env *CommandEnv) *cobra.Command {
	var serverURL, apiKey string
	var enableMemoryWrite, enableAutoSync bool
	var localOnly bool
	cmd := &cobra.Command{
		Use:   "set",
		Short: "Set global OpenViking server URL and API key, then sync to backend",
		RunE: func(c *cobra.Command, _ []string) error {
			serverURL = strings.TrimSpace(serverURL)
			apiKey = strings.TrimSpace(apiKey)
			memoryChanged := c.Flags().Changed("enable-memory-write")
			autoSyncChanged := c.Flags().Changed("enable-auto-sync")
			keyChanged := c.Flags().Changed("key")
			urlChanged := c.Flags().Changed("url")
			if !urlChanged && !keyChanged && !memoryChanged && !autoSyncChanged {
				return errors.New("at least one of --url, --key, --enable-memory-write, --enable-auto-sync must be provided")
			}

			global, err := LoadGlobalConfig()
			if err != nil {
				return err
			}
			if global.OpenViking == nil {
				global.OpenViking = &OpenVikingGlobal{}
			}
			if urlChanged {
				global.OpenViking.ServerURL = serverURL
			}
			if keyChanged {
				if strings.EqualFold(apiKey, "null") {
					global.OpenViking.APIKey = ""
				} else {
					global.OpenViking.APIKey = apiKey
				}
			}
			if err := SaveGlobalConfig(global); err != nil {
				return err
			}

			if localOnly {
				return env.printer().Print(RenderData{
					Brief:  "openviking config saved (local only)",
					Prompt: renderOpenVikingSetLocal(global.OpenViking),
					JSON:   map[string]any{"local": openVikingGlobalPayload(global.OpenViking)},
				})
			}

			body := map[string]any{
				"serverUrl": global.OpenViking.ServerURL,
			}
			if memoryChanged {
				body["enableMemoryWrite"] = enableMemoryWrite
			}
			if autoSyncChanged {
				body["enableAutoSync"] = enableAutoSync
			}
			if keyChanged {
				if strings.EqualFold(apiKey, "null") {
					body["apiKey"] = "null"
				} else {
					body["apiKey"] = apiKey
				}
			}
			client, _, err := env.clientWithToken(true)
			if err != nil {
				return err
			}
			ctx, cancel := env.context()
			defer cancel()
			payload, err := client.PutREST(ctx, "/api/system/openviking/settings", body)
			if err != nil {
				return fmt.Errorf("local saved but backend sync failed: %w", err)
			}
			return env.printer().Print(RenderData{
				Brief:  "openviking config saved and synced",
				Prompt: renderOpenVikingSetRemote(global.OpenViking, payload),
				JSON:   map[string]any{"local": openVikingGlobalPayload(global.OpenViking), "remote": payload},
			})
		},
	}
	cmd.Flags().StringVar(&serverURL, "url", "", "OpenViking server URL")
	cmd.Flags().StringVar(&apiKey, "key", "", "OpenViking API key (use 'null' to clear)")
	cmd.Flags().BoolVar(&enableMemoryWrite, "enable-memory-write", true, "allow memory writes (system-level toggle)")
	cmd.Flags().BoolVar(&enableAutoSync, "enable-auto-sync", true, "allow auto-sync writes (system-level toggle)")
	cmd.Flags().BoolVar(&localOnly, "local-only", false, "skip backend sync; write ~/.aitask/config.json only")
	return cmd
}

// --- project set ---

func newOpenVikingProjectSetCommand(env *CommandEnv) *cobra.Command {
	var namespace, workspaceID string
	var localOnly bool
	cmd := &cobra.Command{
		Use:   "set",
		Short: "Set this project's OpenViking namespace and workspace ID, then sync to backend",
		RunE: func(c *cobra.Command, _ []string) error {
			namespace = strings.TrimSpace(namespace)
			workspaceID = strings.TrimSpace(workspaceID)
			if namespace == "" && workspaceID == "" {
				return errors.New("at least one of --namespace, --workspace must be provided")
			}

			cfg, err := env.resolveProjectConfig(true)
			if err != nil {
				return err
			}

			values := ProjectDocValues{
				ProjectID:             cfg.ProjectID,
				ProjectName:           cfg.ProjectName,
				OpenVikingRoot:        cfg.OpenVikingRoot,
				OpenVikingNamespace:   cfg.OpenVikingNamespace,
				OpenVikingWorkspaceID: cfg.OpenVikingWorkspaceID,
				RoomEnabled:           cfg.RoomEnabled,
			}
			if c.Flags().Changed("namespace") {
				values.OpenVikingNamespace = namespace
			}
			if c.Flags().Changed("workspace") {
				values.OpenVikingWorkspaceID = workspaceID
			}
			if err := writeProjectMarkdown(cfg.SourceFile, values); err != nil {
				return err
			}

			if localOnly {
				return env.printer().Print(RenderData{
					Brief:  "openviking project saved (local only)",
					Prompt: renderOpenVikingProjectLocal(cfg.ProjectID, values),
					JSON:   map[string]any{"projectId": cfg.ProjectID, "namespace": values.OpenVikingNamespace, "workspaceId": values.OpenVikingWorkspaceID},
				})
			}

			body := map[string]any{}
			if c.Flags().Changed("namespace") {
				body["openvikingNamespace"] = namespace
			}
			if c.Flags().Changed("workspace") {
				body["openvikingWorkspaceId"] = workspaceID
			}
			client, _, err := env.clientWithToken(true)
			if err != nil {
				return err
			}
			ctx, cancel := env.context()
			defer cancel()
			payload, err := client.PatchREST(ctx, "/api/projects/"+cfg.ProjectID, body)
			if err != nil {
				return fmt.Errorf("local saved but backend sync failed: %w", err)
			}
			return env.printer().Print(RenderData{
				Brief:  "openviking project saved and synced",
				Prompt: renderOpenVikingProjectRemote(cfg.ProjectID, values, payload),
				JSON:   map[string]any{"projectId": cfg.ProjectID, "local": map[string]any{"namespace": values.OpenVikingNamespace, "workspaceId": values.OpenVikingWorkspaceID}, "remote": payload},
			})
		},
	}
	cmd.Flags().StringVar(&namespace, "namespace", "", "OpenViking namespace for this project")
	cmd.Flags().StringVar(&workspaceID, "workspace", "", "OpenViking workspace ID for this project")
	cmd.Flags().BoolVar(&localOnly, "local-only", false, "skip backend sync; write .aitask/project.md only")
	return cmd
}

// --- import (ovcli.conf -> global + project) ---

func newOpenVikingConfigImportCommand(env *CommandEnv) *cobra.Command {
	var pathFlag string
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import ~/.openviking/ovcli.conf into the layered config (global URL/Key + project Namespace/Workspace)",
		RunE: func(_ *cobra.Command, _ []string) error {
			ovcfg, source, err := loadOpenVikingConf(pathFlag)
			if err != nil {
				if errors.Is(err, ovconf.ErrNotFound) {
					return fmt.Errorf("no ovcli.conf at %s (set %s or create %s)", source, ovconf.EnvConfigPath, ovconf.DefaultRelativePath)
				}
				return err
			}
			if !ovcfg.HasCredentials() {
				return fmt.Errorf("ovcli.conf at %s is missing url or api_key", ovcfg.Source)
			}

			projectCfg, projectErr := env.resolveProjectConfig(false)
			summary := openVikingImportSummary(ovcfg, projectErr == nil, projectCfg)

			if dryRun {
				return env.printer().Print(RenderData{
					Brief:  "dry-run: would split ovcli.conf into global + project",
					Prompt: renderOpenVikingImportPreview(ovcfg, summary),
					JSON:   summary,
				})
			}

			// 1) Write global URL+Key locally and push to backend.
			global, err := LoadGlobalConfig()
			if err != nil {
				return err
			}
			if global.OpenViking == nil {
				global.OpenViking = &OpenVikingGlobal{}
			}
			global.OpenViking.ServerURL = ovcfg.URL
			if ovcfg.APIKey != "" {
				global.OpenViking.APIKey = ovcfg.APIKey
			}
			if err := SaveGlobalConfig(global); err != nil {
				return err
			}
			client, _, err := env.clientWithToken(true)
			if err != nil {
				return err
			}
			ctx, cancel := env.context()
			defer cancel()
			sysBody := map[string]any{"serverUrl": ovcfg.URL}
			if ovcfg.APIKey != "" {
				sysBody["apiKey"] = ovcfg.APIKey
			}
			sysResp, err := client.PutREST(ctx, "/api/system/openviking/settings", sysBody)
			if err != nil {
				return fmt.Errorf("global saved but backend sync failed: %w", err)
			}
			summary["systemRemote"] = sysResp

			// 2) Write project namespace/workspace if we have a project context.
			if projectErr == nil && (strings.TrimSpace(ovcfg.Namespace) != "" || strings.TrimSpace(ovcfg.EffectiveWorkspace()) != "") {
				values := ProjectDocValues{
					ProjectID:             projectCfg.ProjectID,
					ProjectName:           projectCfg.ProjectName,
					OpenVikingRoot:        projectCfg.OpenVikingRoot,
					OpenVikingNamespace:   projectCfg.OpenVikingNamespace,
					OpenVikingWorkspaceID: projectCfg.OpenVikingWorkspaceID,
					RoomEnabled:           projectCfg.RoomEnabled,
				}
				if ns := strings.TrimSpace(ovcfg.Namespace); ns != "" {
					values.OpenVikingNamespace = ns
				}
				if ws := strings.TrimSpace(ovcfg.EffectiveWorkspace()); ws != "" {
					values.OpenVikingWorkspaceID = ws
				}
				if err := writeProjectMarkdown(projectCfg.SourceFile, values); err != nil {
					return err
				}
				patchBody := map[string]any{}
				if strings.TrimSpace(ovcfg.Namespace) != "" {
					patchBody["openvikingNamespace"] = strings.TrimSpace(ovcfg.Namespace)
				}
				if ws := strings.TrimSpace(ovcfg.EffectiveWorkspace()); ws != "" {
					patchBody["openvikingWorkspaceId"] = ws
				}
				if len(patchBody) > 0 {
					projResp, err := client.PatchREST(ctx, "/api/projects/"+projectCfg.ProjectID, patchBody)
					if err != nil {
						return fmt.Errorf("project file saved but backend sync failed: %w", err)
					}
					summary["projectRemote"] = projResp
				}
			}

			return env.printer().Print(RenderData{
				Brief:  fmt.Sprintf("imported %s", ovcfg.Source),
				Prompt: renderOpenVikingImportResult(ovcfg, summary),
				JSON:   summary,
			})
		},
	}
	cmd.Flags().StringVar(&pathFlag, "path", "", "explicit ovcli.conf path (default: $OPENVIKING_CLI_CONFIG_FILE or ~/"+ovconf.DefaultRelativePath+")")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the planned import without writing or contacting the backend")
	return cmd
}

// --- helpers ---

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

func writeProjectMarkdown(path string, values ProjectDocValues) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("project.md path cannot be empty")
	}
	content := RenderProjectMarkdown(values)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func openVikingGlobalPayload(g *OpenVikingGlobal) map[string]any {
	if g == nil {
		return map[string]any{"serverUrl": "", "apiKeySet": false}
	}
	return map[string]any{
		"serverUrl":  g.ServerURL,
		"apiKeySet":  strings.TrimSpace(g.APIKey) != "",
		"apiKeyMask": ovconf.MaskedAPIKey(g.APIKey),
	}
}

func mergedOpenVikingPayload(global GlobalConfig, project ProjectConfig, hasProject bool) map[string]any {
	out := map[string]any{
		"global": openVikingGlobalPayload(global.OpenViking),
	}
	if hasProject {
		out["project"] = map[string]any{
			"projectId":   project.ProjectID,
			"namespace":   project.OpenVikingNamespace,
			"workspaceId": project.OpenVikingWorkspaceID,
			"source":      project.SourceFile,
		}
	} else {
		out["project"] = nil
	}
	return out
}

func briefOpenVikingMerged(merged map[string]any) string {
	g, _ := merged["global"].(map[string]any)
	url := ""
	if g != nil {
		if v, ok := g["serverUrl"].(string); ok {
			url = v
		}
	}
	if url == "" {
		return "global url not set"
	}
	return url
}

func openVikingImportSummary(cfg ovconf.Config, hasProject bool, project ProjectConfig) map[string]any {
	out := map[string]any{
		"source": cfg.Source,
		"global": map[string]any{
			"serverUrl":  cfg.URL,
			"apiKeySet":  cfg.APIKey != "",
			"apiKeyMask": ovconf.MaskedAPIKey(cfg.APIKey),
		},
	}
	if hasProject {
		out["project"] = map[string]any{
			"projectId":   project.ProjectID,
			"namespace":   strings.TrimSpace(cfg.Namespace),
			"workspaceId": strings.TrimSpace(cfg.EffectiveWorkspace()),
			"source":      project.SourceFile,
		}
	} else {
		out["project"] = nil
		out["projectError"] = "no .aitask/project.md found upward from CWD; project-side import skipped"
	}
	return out
}

func renderOpenVikingShow(merged map[string]any) string {
	var sb strings.Builder
	sb.WriteString("OpenViking Configuration\n\n")
	if g, ok := merged["global"].(map[string]any); ok && g != nil {
		sb.WriteString("Global (~/.aitask/config.json)\n")
		sb.WriteString(fmt.Sprintf("  serverUrl: %s\n", g["serverUrl"]))
		mask, _ := g["apiKeyMask"].(string)
		set, _ := g["apiKeySet"].(bool)
		sb.WriteString(fmt.Sprintf("  apiKey:    %s (set=%t)\n", mask, set))
	}
	if p, ok := merged["project"].(map[string]any); ok && p != nil {
		sb.WriteString("\nProject (.aitask/project.md)\n")
		sb.WriteString(fmt.Sprintf("  projectId:   %s\n", p["projectId"]))
		sb.WriteString(fmt.Sprintf("  namespace:   %s\n", p["namespace"]))
		sb.WriteString(fmt.Sprintf("  workspaceId: %s\n", p["workspaceId"]))
	} else {
		sb.WriteString("\nProject: (no .aitask/project.md found)\n")
	}
	if r, ok := merged["remote"]; ok && r != nil {
		sb.WriteString("\nBackend system settings (live)\n")
		sb.WriteString(fmt.Sprintf("  %+v\n", r))
	}
	if e, ok := merged["remoteError"].(string); ok && e != "" {
		sb.WriteString("\nBackend reconciliation error: " + e + "\n")
	}
	return sb.String()
}

func renderOpenVikingSetLocal(g *OpenVikingGlobal) string {
	if g == nil {
		return "OpenViking global config cleared.\n"
	}
	return fmt.Sprintf("OpenViking global config saved locally.\nserverUrl: %s\napiKey:    %s\n", g.ServerURL, ovconf.MaskedAPIKey(g.APIKey))
}

func renderOpenVikingSetRemote(g *OpenVikingGlobal, payload map[string]any) string {
	out := renderOpenVikingSetLocal(g)
	out += "\nBackend response:\n"
	for k, v := range payload {
		out += fmt.Sprintf("  %s: %v\n", k, v)
	}
	return out
}

func renderOpenVikingProjectLocal(projectID string, values ProjectDocValues) string {
	return fmt.Sprintf("OpenViking project config saved locally.\nproject:     %s\nnamespace:   %s\nworkspaceId: %s\n", projectID, values.OpenVikingNamespace, values.OpenVikingWorkspaceID)
}

func renderOpenVikingProjectRemote(projectID string, values ProjectDocValues, payload map[string]any) string {
	return renderOpenVikingProjectLocal(projectID, values) + fmt.Sprintf("\nBackend updatedAt: %v\n", payload["updatedAt"])
}

func renderOpenVikingImportPreview(cfg ovconf.Config, summary map[string]any) string {
	var sb strings.Builder
	sb.WriteString("Dry-run: not writing files or contacting backend.\n")
	sb.WriteString("Source: " + cfg.Source + "\n\n")
	sb.WriteString("Would write global (~/.aitask/config.json):\n")
	sb.WriteString(fmt.Sprintf("  serverUrl: %s\n", cfg.URL))
	sb.WriteString(fmt.Sprintf("  apiKey:    %s\n", ovconf.MaskedAPIKey(cfg.APIKey)))
	if p, ok := summary["project"].(map[string]any); ok && p != nil {
		sb.WriteString("\nWould write project (.aitask/project.md):\n")
		sb.WriteString(fmt.Sprintf("  namespace:   %s\n", p["namespace"]))
		sb.WriteString(fmt.Sprintf("  workspaceId: %s\n", p["workspaceId"]))
	} else if e, ok := summary["projectError"].(string); ok {
		sb.WriteString("\nProject-side skipped: " + e + "\n")
	}
	return sb.String()
}

func renderOpenVikingImportResult(cfg ovconf.Config, summary map[string]any) string {
	var sb strings.Builder
	sb.WriteString("Imported OpenViking config.\n")
	sb.WriteString("Source: " + cfg.Source + "\n\n")
	sb.WriteString("Global synced: serverUrl=" + cfg.URL + "\n")
	if p, ok := summary["project"].(map[string]any); ok && p != nil {
		sb.WriteString(fmt.Sprintf("Project synced: project=%s namespace=%s workspaceId=%s\n", p["projectId"], p["namespace"], p["workspaceId"]))
	} else if e, ok := summary["projectError"].(string); ok {
		sb.WriteString("Project-side skipped: " + e + "\n")
	}
	return sb.String()
}

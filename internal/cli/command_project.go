package cli

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func newInitCommand(env *CommandEnv) *cobra.Command {
	var projectID string
	var agentsFlag string
	var nameFlag string
	var goalFlag string
	var descriptionFlag string
	var noSync bool
	var installGitHook bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize local .aitask workspace",
		RunE: func(_ *cobra.Command, _ []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			resolved := strings.TrimSpace(projectID)
			if resolved == "" {
				resolved = strings.TrimSpace(env.opts.projectID)
			}
			kinds, err := ParseAgentKinds(agentsFlag)
			if err != nil {
				return err
			}
			values := ProjectDocValues{ProjectID: resolved, RoomEnabled: true}
			createPayload := map[string]any{}
			if resolved == "" {
				created, err := createProjectFromRepository(env, cwd, nameFlag, goalFlag, descriptionFlag)
				if err != nil {
					return err
				}
				createPayload = created
				resolved = mapString(created, "projectId")
				if resolved == "" {
					return fmt.Errorf("backend create project returned an empty projectId")
				}
			}
			values.ProjectID = resolved
			if fetched, err := fetchProjectDocValues(env, resolved); err == nil {
				values = fetched
			}
			var workspacePatchError string
			if values.OpenVikingWorkspaceID == "" {
				values.OpenVikingWorkspaceID = stableWorkspaceID(cwd)
				if _, err := patchProjectOpenVikingWorkspace(env, values.ProjectID, values.OpenVikingWorkspaceID); err != nil {
					workspacePatchError = err.Error()
				}
			}
			created, err := InitProjectFiles(cwd, values)
			if err != nil {
				return err
			}
			if err := BindProject(cwd, values); err != nil {
				return err
			}
			agentFiles, err := WriteAgentContextFiles(cwd, kinds, values)
			if err != nil {
				return err
			}
			created = append(created, agentFiles...)

			var syncResult map[string]any
			if !noSync {
				if result, err := syncProjectIndex(env, cwd, values, false); err != nil {
					syncResult = map[string]any{"synced": false, "error": err.Error()}
				} else {
					syncResult = result
				}
			}
			var hookPath string
			if installGitHook {
				hookPath, err = installProjectSyncGitHook(cwd)
				if err != nil {
					return err
				}
			}

			aiDir := filepath.Join(cwd, AITaskDirName)
			prompt := fmt.Sprintf("# AITask Init\n\nProject `%s` initialized in `%s`.\n", resolved, aiDir)
			if len(created) == 0 {
				prompt += "\nAll required files already existed."
			} else {
				prompt += "\nCreated or updated files:\n"
				for _, path := range created {
					prompt += "- " + path + "\n"
				}
			}
			if syncResult != nil {
				if mapBool(syncResult, "synced") {
					prompt += "\nOpenViking project index synced."
				} else if errText := mapString(syncResult, "error"); errText != "" {
					prompt += "\nOpenViking project index sync failed: " + errText
				}
			}
			if hookPath != "" {
				prompt += "\nInstalled git sync hook: `" + hookPath + "`"
			}
			if workspacePatchError != "" {
				prompt += "\nOpenViking workspace metadata was written locally but backend PATCH failed: " + workspacePatchError
			}
			jsonOut := map[string]any{
				"projectId":       resolved,
				"rootDir":         cwd,
				"aitaskDir":       aiDir,
				"created":         created,
				"agentFiles":      agentFiles,
				"agentsRequested": kinds,
				"createdProject":  createPayload,
				"openvikingSync":  syncResult,
				"gitHook":         hookPath,
				"workspaceError":  workspacePatchError,
			}
			return env.printer().Print(RenderData{Brief: "init complete", Prompt: prompt, JSON: jsonOut})
		},
	}
	cmd.Flags().StringVar(&projectID, "project", "", "project ID")
	cmd.Flags().StringVar(&agentsFlag, "agents", "",
		"comma-separated agents to configure (claude,codex,gemini; empty = all)")
	cmd.Flags().StringVar(&nameFlag, "name", "", "project name when auto-creating a backend project")
	cmd.Flags().StringVar(&goalFlag, "goal", "", "project goal when auto-creating a backend project")
	cmd.Flags().StringVar(&descriptionFlag, "description", "", "project description when auto-creating a backend project")
	cmd.Flags().BoolVar(&noSync, "no-sync", false, "skip initial OpenViking repository index write")
	cmd.Flags().BoolVar(&installGitHook, "install-git-hook", false, "install .git/hooks/post-commit to run aitask project sync-code")
	return cmd
}

func newProjectCommand(env *CommandEnv) *cobra.Command {
	cmd := &cobra.Command{Use: "project", Short: "Project binding and info"}

	bindCmd := &cobra.Command{
		Use:   "bind <project_id>",
		Short: "Bind project to current repository",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			projectID := strings.TrimSpace(args[0])
			if projectID == "" {
				return fmt.Errorf("project_id cannot be empty")
			}
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			values := ProjectDocValues{ProjectID: projectID, RoomEnabled: true}
			if fetched, err := fetchProjectDocValues(env, projectID); err == nil {
				values = fetched
			}
			if err := BindProject(cwd, values); err != nil {
				return err
			}
			prompt := fmt.Sprintf("# Project Bound\n\nBound `%s` to `%s`.", projectID, cwd)
			jsonOut := map[string]any{"projectId": projectID, "bound": true, "rootDir": cwd}
			return env.printer().Print(RenderData{Brief: "project bound", Prompt: prompt, JSON: jsonOut})
		},
	}

	useCmd := &cobra.Command{
		Use:   "use <project_id>",
		Short: "Switch active project in current repository",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			projectID := strings.TrimSpace(args[0])
			file, err := UseProject(cwd, projectID)
			if err != nil {
				return err
			}
			prompt := fmt.Sprintf("# Active Project Switched\n\nNow using `%s`.\n\nSource: `%s`", projectID, file)
			jsonOut := map[string]any{"projectId": projectID, "source": file}
			return env.printer().Print(RenderData{Brief: "project switched", Prompt: prompt, JSON: jsonOut})
		},
	}

	infoCmd := &cobra.Command{
		Use:   "info",
		Short: "Show current and bound projects",
		RunE: func(_ *cobra.Command, _ []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			cfg, err := LoadProjectConfig(cwd)
			if err != nil {
				return err
			}
			items, err := ListBoundProjects(cfg.RootDir)
			if err != nil {
				return err
			}
			available := make([]string, 0, len(items))
			for _, item := range items {
				available = append(available, item.ProjectID)
			}
			prompt := fmt.Sprintf("# Project Info\n\n- Current Project ID: `%s`\n- Project Name: `%s`\n- OpenViking Root: `%s`\n- OpenViking Namespace: `%s`\n- OpenViking Workspace ID: `%s`\n- Room Enabled: `%t`\n- Bound Projects: %s", cfg.ProjectID, cfg.ProjectName, cfg.OpenVikingRoot, cfg.OpenVikingNamespace, cfg.OpenVikingWorkspaceID, cfg.RoomEnabled, strings.Join(available, ", "))
			jsonOut := map[string]any{
				"current": map[string]any{
					"projectId":             cfg.ProjectID,
					"projectName":           cfg.ProjectName,
					"openvikingRoot":        cfg.OpenVikingRoot,
					"openvikingNamespace":   cfg.OpenVikingNamespace,
					"openvikingWorkspaceId": cfg.OpenVikingWorkspaceID,
					"roomEnabled":           cfg.RoomEnabled,
					"source":                cfg.SourceFile,
				},
				"boundProjects": available,
			}
			return env.printer().Print(RenderData{Brief: cfg.ProjectID, Prompt: prompt, JSON: jsonOut})
		},
	}

	syncCmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync repository project index to OpenViking",
		RunE: func(_ *cobra.Command, _ []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			cfg, err := LoadProjectConfig(cwd)
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
			payload, err := syncProjectIndex(env, cfg.RootDir, values, true)
			if err != nil {
				return err
			}
			return env.printer().Print(RenderData{
				Brief:  "project index synced",
				Prompt: renderProjectSyncPrompt(payload),
				JSON:   payload,
			})
		},
	}

	var syncCodeRemote string
	var syncCodeURL string
	var syncCodeTargetURI string
	var syncCodeReason string
	var syncCodeWatchInterval int
	var syncCodeWait bool
	var syncCodeStatusTaskID string
	var syncCodeStatusTargetURI string
	var syncCodeStatusLimit int
	syncCodeCmd := &cobra.Command{
		Use:   "sync-code",
		Short: "Register current git repository as an OpenViking resource",
		RunE: func(_ *cobra.Command, _ []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			cfg, err := LoadProjectConfig(cwd)
			if err != nil {
				return err
			}
			payload, err := syncProjectGitResource(env, cfg, syncProjectGitOptions{
				Remote:        syncCodeRemote,
				RepositoryURL: syncCodeURL,
				TargetURI:     syncCodeTargetURI,
				Reason:        syncCodeReason,
				WatchInterval: syncCodeWatchInterval,
				Wait:          syncCodeWait,
			})
			if err != nil {
				return err
			}
			return env.printer().Print(RenderData{
				Brief:  "git resource synced",
				Prompt: renderProjectSyncCodePrompt(payload),
				JSON:   payload,
			})
		},
	}
	syncCodeCmd.Flags().StringVar(&syncCodeRemote, "remote", "origin", "git remote name")
	syncCodeCmd.Flags().StringVar(&syncCodeURL, "url", "", "git repository URL override")
	syncCodeCmd.Flags().StringVar(&syncCodeTargetURI, "to", "", "OpenViking resource target URI")
	syncCodeCmd.Flags().StringVar(&syncCodeReason, "reason", "AITask source repository", "OpenViking resource sync reason")
	syncCodeCmd.Flags().IntVar(&syncCodeWatchInterval, "watch-interval", 5, "OpenViking watch interval in minutes; 0 disables watch")
	syncCodeCmd.Flags().BoolVar(&syncCodeWait, "wait", false, "wait for OpenViking ingestion to finish")
	syncCodeStatusCmd := &cobra.Command{
		Use:   "status",
		Short: "Show OpenViking git resource sync progress",
		RunE: func(_ *cobra.Command, _ []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			cfg, err := LoadProjectConfig(cwd)
			if err != nil {
				return err
			}
			payload, err := syncProjectGitStatus(env, cfg, syncProjectGitStatusOptions{
				TaskID:    syncCodeStatusTaskID,
				TargetURI: syncCodeStatusTargetURI,
				Limit:     syncCodeStatusLimit,
			})
			if err != nil {
				return err
			}
			return env.printer().Print(RenderData{
				Brief:  renderProjectSyncCodeStatusBrief(payload),
				Prompt: renderProjectSyncCodeStatusPrompt(payload),
				JSON:   payload,
			})
		},
	}
	syncCodeStatusCmd.Flags().StringVar(&syncCodeStatusTaskID, "task-id", "", "OpenViking task id")
	syncCodeStatusCmd.Flags().StringVar(&syncCodeStatusTargetURI, "to", "", "OpenViking resource target URI")
	syncCodeStatusCmd.Flags().IntVar(&syncCodeStatusLimit, "limit", 20, "maximum task records to show")
	syncCodeCmd.AddCommand(syncCodeStatusCmd)

	cmd.AddCommand(bindCmd, useCmd, infoCmd, syncCmd, syncCodeCmd)
	return cmd
}

func createProjectFromRepository(env *CommandEnv, rootDir, nameFlag, goalFlag, descriptionFlag string) (map[string]any, error) {
	client, _, err := env.clientWithToken(false)
	if err != nil {
		return nil, err
	}
	meta := detectRepositoryMetadata(rootDir)
	name := firstNonEmpty(nameFlag, meta.Name, filepath.Base(rootDir))
	goal := firstNonEmpty(goalFlag, fmt.Sprintf("Maintain and deliver the %s repository with AITask.", name))
	description := firstNonEmpty(descriptionFlag, meta.Description)
	ctx, cancel := env.context()
	defer cancel()
	payload, err := client.PostREST(ctx, "/api/projects", map[string]any{
		"name":        truncateRunes(name, 80),
		"goal":        truncateRunes(goal, 200),
		"description": truncateRunes(description, 500),
	})
	if err != nil {
		return nil, fmt.Errorf("auto-create backend project failed: %w", err)
	}
	return payload, nil
}

type repositoryMetadata struct {
	Name        string
	Description string
	Branch      string
	Remote      string
	Commit      string
}

func detectRepositoryMetadata(rootDir string) repositoryMetadata {
	name := filepath.Base(rootDir)
	if top := gitOutput(rootDir, "rev-parse", "--show-toplevel"); top != "" {
		name = filepath.Base(top)
	}
	meta := repositoryMetadata{
		Name:        name,
		Description: fmt.Sprintf("Repository path: %s", rootDir),
		Branch:      gitOutput(rootDir, "branch", "--show-current"),
		Remote:      gitOutput(rootDir, "config", "--get", "remote.origin.url"),
		Commit:      gitOutput(rootDir, "rev-parse", "--short", "HEAD"),
	}
	if meta.Remote != "" {
		meta.Description = fmt.Sprintf("Repository %s (%s)", name, meta.Remote)
	}
	return meta
}

func syncProjectIndex(env *CommandEnv, rootDir string, values ProjectDocValues, manual bool) (map[string]any, error) {
	values = normalizeProjectDocValues(values)
	if values.ProjectID == "" {
		return nil, fmt.Errorf("project_id cannot be empty")
	}
	client, _, err := env.clientWithToken(false)
	if err != nil {
		return nil, err
	}
	content := renderRepositoryIndex(rootDir, values)
	title := "repository-index"
	ctx, cancel := env.context()
	defer cancel()
	payload, err := client.PostREST(ctx, "/api/projects/"+values.ProjectID+"/memory/write", map[string]any{
		"target":   "resources",
		"title":    title,
		"content":  content,
		"autoSync": true,
	})
	if err != nil {
		return nil, err
	}
	payload["synced"] = true
	payload["projectId"] = values.ProjectID
	payload["title"] = title
	payload["rootDir"] = rootDir
	payload["fingerprint"] = repositoryFingerprint(rootDir)
	return payload, nil
}

type syncProjectGitOptions struct {
	Remote        string
	RepositoryURL string
	TargetURI     string
	Reason        string
	WatchInterval int
	Wait          bool
}

type syncProjectGitStatusOptions struct {
	TaskID    string
	TargetURI string
	Limit     int
}

func syncProjectGitResource(env *CommandEnv, cfg ProjectConfig, opts syncProjectGitOptions) (map[string]any, error) {
	projectID := strings.TrimSpace(cfg.ProjectID)
	if projectID == "" {
		return nil, fmt.Errorf("project_id cannot be empty")
	}
	rootDir := strings.TrimSpace(cfg.RootDir)
	if rootDir == "" {
		return nil, fmt.Errorf("project root cannot be empty")
	}
	if opts.WatchInterval < 0 {
		return nil, fmt.Errorf("watch-interval cannot be negative")
	}
	remoteName := firstNonEmpty(opts.Remote, "origin")
	repoURL := strings.TrimSpace(opts.RepositoryURL)
	if repoURL == "" {
		repoURL = gitOutput(rootDir, "remote", "get-url", remoteName)
	}
	if repoURL == "" {
		return nil, fmt.Errorf("git remote %q has no URL; pass --url", remoteName)
	}
	branch := gitOutput(rootDir, "branch", "--show-current")
	commit := gitOutput(rootDir, "rev-parse", "HEAD")
	targetURI := strings.TrimSpace(opts.TargetURI)
	if targetURI == "" {
		targetURI = defaultGitResourceURI(cfg, repoURL, rootDir)
	}
	client, _, err := env.clientWithToken(false)
	if err != nil {
		return nil, err
	}
	body := map[string]any{
		"repositoryUrl": repoURL,
		"targetUri":     targetURI,
		"watchInterval": opts.WatchInterval,
		"wait":          opts.Wait,
	}
	if reason := strings.TrimSpace(opts.Reason); reason != "" {
		body["reason"] = reason
	}
	if branch != "" {
		body["branch"] = branch
	}
	if commit != "" {
		body["commit"] = commit
	}
	ctx, cancel := env.context()
	defer cancel()
	payload, err := client.PostREST(ctx, "/api/projects/"+projectID+"/openviking/resources/git", body)
	if err != nil {
		return nil, err
	}
	payload["synced"] = true
	payload["projectId"] = projectID
	payload["repositoryUrl"] = repoURL
	payload["targetUri"] = firstNonEmpty(mapString(payload, "uri"), targetURI)
	payload["watchInterval"] = opts.WatchInterval
	payload["branch"] = branch
	payload["commit"] = commit
	payload["remote"] = remoteName
	return payload, nil
}

func syncProjectGitStatus(env *CommandEnv, cfg ProjectConfig, opts syncProjectGitStatusOptions) (map[string]any, error) {
	projectID := strings.TrimSpace(cfg.ProjectID)
	if projectID == "" {
		return nil, fmt.Errorf("project_id cannot be empty")
	}
	targetURI := strings.TrimSpace(opts.TargetURI)
	if targetURI == "" {
		targetURI = defaultGitResourceURI(cfg, gitOutput(cfg.RootDir, "remote", "get-url", "origin"), cfg.RootDir)
	}
	query := map[string]string{
		"targetUri": targetURI,
	}
	if taskID := strings.TrimSpace(opts.TaskID); taskID != "" {
		query["taskId"] = taskID
	}
	if opts.Limit > 0 {
		query["limit"] = fmt.Sprintf("%d", opts.Limit)
	}
	client, _, err := env.clientWithToken(false)
	if err != nil {
		return nil, err
	}
	ctx, cancel := env.context()
	defer cancel()
	payload, err := client.GetREST(ctx, "/api/projects/"+projectID+"/openviking/resources/git/status", query)
	if err != nil {
		return nil, err
	}
	payload["projectId"] = projectID
	if mapString(payload, "targetUri") == "" {
		payload["targetUri"] = targetURI
	}
	return payload, nil
}

func defaultGitResourceURI(cfg ProjectConfig, repoURL string, rootDir string) string {
	name := strings.TrimSpace(cfg.ProjectName)
	if name == "" {
		name = repositoryNameFromURL(repoURL)
	}
	if name == "" {
		name = filepath.Base(rootDir)
	}
	return "viking://resources/" + sanitizeResourceName(name)
}

func repositoryNameFromURL(repoURL string) string {
	value := strings.TrimSpace(repoURL)
	if value == "" {
		return ""
	}
	value = strings.TrimSuffix(value, "/")
	if idx := strings.LastIndex(value, "/"); idx >= 0 {
		value = value[idx+1:]
	}
	if idx := strings.LastIndex(value, ":"); idx >= 0 {
		value = value[idx+1:]
	}
	return strings.TrimSuffix(value, ".git")
}

func sanitizeResourceName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case r == '-' || r == '_' || r == '.':
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "repository"
	}
	return out
}

func patchProjectOpenVikingWorkspace(env *CommandEnv, projectID, workspaceID string) (map[string]any, error) {
	projectID = strings.TrimSpace(projectID)
	workspaceID = strings.TrimSpace(workspaceID)
	if projectID == "" || workspaceID == "" {
		return nil, nil
	}
	client, _, err := env.clientWithToken(false)
	if err != nil {
		return nil, err
	}
	ctx, cancel := env.context()
	defer cancel()
	payload, err := client.PatchREST(ctx, "/api/projects/"+projectID, map[string]any{
		"openvikingWorkspaceId": workspaceID,
	})
	if err != nil {
		return nil, fmt.Errorf("set project openviking workspace failed: %w", err)
	}
	return payload, nil
}

func renderRepositoryIndex(rootDir string, values ProjectDocValues) string {
	meta := detectRepositoryMetadata(rootDir)
	files := repositoryFiles(rootDir, 80)
	var b strings.Builder
	b.WriteString("# Repository Index\n\n")
	b.WriteString("- Synced at: " + time.Now().UTC().Format(time.RFC3339) + "\n")
	b.WriteString("- Project ID: " + values.ProjectID + "\n")
	b.WriteString("- Project Name: " + firstNonEmpty(values.ProjectName, meta.Name) + "\n")
	b.WriteString("- OpenViking Root: " + values.OpenVikingRoot + "\n")
	if values.OpenVikingNamespace != "" {
		b.WriteString("- OpenViking Namespace: " + values.OpenVikingNamespace + "\n")
	}
	if values.OpenVikingWorkspaceID != "" {
		b.WriteString("- OpenViking Workspace ID: " + values.OpenVikingWorkspaceID + "\n")
	}
	if meta.Branch != "" {
		b.WriteString("- Git Branch: " + meta.Branch + "\n")
	}
	if meta.Commit != "" {
		b.WriteString("- Git Commit: " + meta.Commit + "\n")
	}
	if meta.Remote != "" {
		b.WriteString("- Git Remote: " + meta.Remote + "\n")
	}
	b.WriteString("\n## Repository Files\n\n")
	if len(files) == 0 {
		b.WriteString("No files detected.\n")
	} else {
		for _, file := range files {
			b.WriteString("- " + file + "\n")
		}
	}
	b.WriteString("\n## Update Contract\n\n")
	b.WriteString("Run `aitask project sync` after meaningful repository updates to refresh this OpenViking index.\n")
	return b.String()
}

func repositoryFiles(rootDir string, limit int) []string {
	if limit <= 0 {
		limit = 80
	}
	out := splitLines(gitOutput(rootDir, "ls-files"))
	if len(out) == 0 {
		entries, _ := os.ReadDir(rootDir)
		for _, entry := range entries {
			name := entry.Name()
			if strings.HasPrefix(name, ".git") || name == AITaskDirName {
				continue
			}
			out = append(out, name)
		}
	}
	filtered := make([]string, 0, len(out))
	for _, file := range out {
		file = strings.TrimSpace(file)
		if file == "" || strings.HasPrefix(file, ".git/") {
			continue
		}
		filtered = append(filtered, file)
	}
	sort.Strings(filtered)
	if len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered
}

func repositoryFingerprint(rootDir string) string {
	parts := []string{
		gitOutput(rootDir, "rev-parse", "HEAD"),
		gitOutput(rootDir, "status", "--short"),
		strings.Join(repositoryFiles(rootDir, 500), "\n"),
	}
	sum := sha1.Sum([]byte(strings.Join(parts, "\n---\n")))
	return hex.EncodeToString(sum[:])
}

func stableWorkspaceID(rootDir string) string {
	seed := firstNonEmpty(gitOutput(rootDir, "config", "--get", "remote.origin.url"), gitOutput(rootDir, "rev-parse", "--show-toplevel"), rootDir)
	sum := sha1.Sum([]byte(seed))
	return "ws_" + hex.EncodeToString(sum[:8])
}

func installProjectSyncGitHook(rootDir string) (string, error) {
	gitDir := gitOutput(rootDir, "rev-parse", "--git-dir")
	if gitDir == "" {
		return "", fmt.Errorf("--install-git-hook requires a git repository")
	}
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(rootDir, gitDir)
	}
	hookPath := filepath.Join(gitDir, "hooks", "post-commit")
	if err := os.MkdirAll(filepath.Dir(hookPath), 0o755); err != nil {
		return "", err
	}
	const marker = "# aitask project sync-code"
	snippet := "\n" + marker + "\nif command -v aitask >/dev/null 2>&1; then\n  aitask project sync-code --format brief >/dev/null 2>&1 || true\nfi\n"
	existing, _ := os.ReadFile(hookPath)
	if strings.Contains(string(existing), marker) {
		return hookPath, nil
	}
	if len(existing) == 0 {
		existing = []byte("#!/bin/sh\n")
	}
	if err := os.WriteFile(hookPath, append(existing, []byte(snippet)...), 0o755); err != nil {
		return "", err
	}
	return hookPath, nil
}

func renderProjectSyncPrompt(payload map[string]any) string {
	return fmt.Sprintf("# Project Sync\n\nOpenViking repository index synced for project `%s`.\n\n- URI: `%s`\n- Fingerprint: `%s`", mapString(payload, "projectId"), mapString(payload, "uri"), mapString(payload, "fingerprint"))
}

func renderProjectSyncCodePrompt(payload map[string]any) string {
	uri := firstNonEmpty(mapString(payload, "uri"), mapString(payload, "targetUri"))
	return fmt.Sprintf("# Project Code Sync\n\nOpenViking git resource registered for project `%s`.\n\n- Repository: `%s`\n- URI: `%s`\n- Branch: `%s`\n- Commit: `%s`\n- Watch Interval: `%d` minutes", mapString(payload, "projectId"), mapString(payload, "repositoryUrl"), uri, mapString(payload, "branch"), mapString(payload, "commit"), mapInt(payload, "watchInterval"))
}

func renderProjectSyncCodeStatusBrief(payload map[string]any) string {
	current, _ := payload["current"].(map[string]any)
	status := ""
	if current != nil {
		status = mapString(current, "status")
	}
	if status == "" && mapBool(payload, "monitored") {
		status = "monitored"
	}
	if status == "" && mapBool(payload, "indexed") {
		status = "indexed"
	}
	if status == "" {
		status = "not monitored"
	}
	return status
}

func renderProjectSyncCodeStatusPrompt(payload map[string]any) string {
	var b strings.Builder
	b.WriteString("# Project Code Sync Status\n\n")
	b.WriteString("- Project ID: `" + mapString(payload, "projectId") + "`\n")
	b.WriteString("- Target URI: `" + mapString(payload, "targetUri") + "`\n")
	b.WriteString(fmt.Sprintf("- Monitored: `%t`\n", mapBool(payload, "monitored")))
	b.WriteString(fmt.Sprintf("- Indexed: `%t`\n", mapBool(payload, "indexed")))
	if status := mapString(payload, "status"); status != "" {
		b.WriteString("- Status: `" + status + "`\n")
	}
	if watchTaskID := mapString(payload, "watchTaskId"); watchTaskID != "" {
		b.WriteString("- Watch Task: `" + watchTaskID + "`\n")
	}
	if current, ok := payload["current"].(map[string]any); ok {
		b.WriteString("- Current Task: `" + mapString(current, "taskId") + "`\n")
		b.WriteString("- Status: `" + mapString(current, "status") + "`\n")
		if taskType := mapString(current, "taskType"); taskType != "" {
			b.WriteString("- Task Type: `" + taskType + "`\n")
		}
	}
	if items := asSlice(payload["items"]); len(items) > 0 {
		b.WriteString(fmt.Sprintf("\nTasks: `%d`\n", len(items)))
	}
	if note := mapString(payload, "note"); note != "" {
		b.WriteString("\nNote: " + note + "\n")
	}
	return b.String()
}

func gitOutput(rootDir string, args ...string) string {
	cmd := exec.Command("git", args...)
	cmd.Dir = rootDir
	raw, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

func splitLines(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	lines := strings.Split(value, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func truncateRunes(value string, max int) string {
	value = strings.TrimSpace(value)
	if max <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max])
}

func fetchProjectDocValues(env *CommandEnv, projectID string) (ProjectDocValues, error) {
	client, _, err := env.clientWithToken(false)
	if err != nil {
		return ProjectDocValues{}, err
	}
	ctx, cancel := env.context()
	defer cancel()
	payload, err := client.GetREST(ctx, "/api/projects/"+projectID, nil)
	if err != nil {
		return ProjectDocValues{}, err
	}
	return ProjectDocValues{
		ProjectID:             projectID,
		ProjectName:           mapString(payload, "name"),
		OpenVikingRoot:        mapString(payload, "openvikingRoot"),
		OpenVikingNamespace:   mapString(payload, "openvikingNamespace"),
		OpenVikingWorkspaceID: mapString(payload, "openvikingWorkspaceId"),
		RoomEnabled:           mapString(payload, "roomId") != "",
	}, nil
}

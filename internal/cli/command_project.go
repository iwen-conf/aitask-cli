package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

func newInitCommand(env *CommandEnv) *cobra.Command {
	var projectID string
	var agentsFlag string
	var nameFlag string
	var goalFlag string
	var descriptionFlag string
	var hooksFlag bool
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

			var hookResult HookEnsureResult
			if hooksFlag {
				hookResult, err = EnsureAgentHooks(cwd, kinds)
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
			prompt += formatHookEnsureResult(hookResult)
			jsonOut := map[string]any{
				"projectId":       resolved,
				"rootDir":         cwd,
				"aitaskDir":       aiDir,
				"created":         created,
				"agentFiles":      agentFiles,
				"agentsRequested": kinds,
				"createdProject":  createPayload,
				"hooks": map[string]any{
					"alreadyInstalled": hookResult.AlreadyInstalled,
					"installed":        hookResult.Installed,
					"failed":           hookResult.Failed,
					"installScript":    hookResult.InstallScript,
					"notes":            hookResult.Notes,
				},
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
	cmd.Flags().BoolVar(&hooksFlag, "hooks", true,
		"install SessionStart + per-turn hooks for the selected agents (requires scripts/aitask-hooks/install.sh)")
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
			prompt := fmt.Sprintf("# Project Info\n\n- Current Project ID: `%s`\n- Project Name: `%s`\n- Room Enabled: `%t`\n- Bound Projects: %s", cfg.ProjectID, cfg.ProjectName, cfg.RoomEnabled, strings.Join(available, ", "))
			jsonOut := map[string]any{
				"current": map[string]any{
					"projectId":   cfg.ProjectID,
					"projectName": cfg.ProjectName,
					"roomEnabled": cfg.RoomEnabled,
					"source":      cfg.SourceFile,
				},
				"boundProjects": available,
			}
			return env.printer().Print(RenderData{Brief: cfg.ProjectID, Prompt: prompt, JSON: jsonOut})
		},
	}

	cmd.AddCommand(bindCmd, useCmd, infoCmd)
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

func gitOutput(rootDir string, args ...string) string {
	cmd := exec.Command("git", args...)
	cmd.Dir = rootDir
	raw, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
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
		ProjectID:   projectID,
		ProjectName: mapString(payload, "name"),
		RoomEnabled: mapString(payload, "roomId") != "",
	}, nil
}

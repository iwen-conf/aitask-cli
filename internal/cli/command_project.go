package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

func newInitCommand(env *CommandEnv) *cobra.Command {
	var projectID string
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize local .aitask workspace",
		RunE: func(_ *cobra.Command, _ []string) error {
			resolved := strings.TrimSpace(projectID)
			if resolved == "" {
				resolved = strings.TrimSpace(env.opts.projectID)
			}
			if resolved == "" {
				return fmt.Errorf("project_id is required: use --project <project_id>")
			}
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			values := ProjectDocValues{ProjectID: resolved, RoomEnabled: true}
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

			aiDir := filepath.Join(cwd, AITaskDirName)
			prompt := fmt.Sprintf("# AITask Init\n\nProject `%s` initialized in `%s`.\n", resolved, aiDir)
			if len(created) == 0 {
				prompt += "\nAll required files already existed."
			} else {
				prompt += "\nCreated files:\n"
				for _, path := range created {
					prompt += "- " + path + "\n"
				}
			}
			jsonOut := map[string]any{"projectId": resolved, "rootDir": cwd, "aitaskDir": aiDir, "created": created}
			return env.printer().Print(RenderData{Brief: "init complete", Prompt: prompt, JSON: jsonOut})
		},
	}
	cmd.Flags().StringVar(&projectID, "project", "", "project ID")
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
			prompt := fmt.Sprintf("# Project Info\n\n- Current Project ID: `%s`\n- Project Name: `%s`\n- OpenViking Root: `%s`\n- Room Enabled: `%t`\n- Bound Projects: %s", cfg.ProjectID, cfg.ProjectName, cfg.OpenVikingRoot, cfg.RoomEnabled, strings.Join(available, ", "))
			jsonOut := map[string]any{
				"current": map[string]any{
					"projectId":      cfg.ProjectID,
					"projectName":    cfg.ProjectName,
					"openvikingRoot": cfg.OpenVikingRoot,
					"roomEnabled":    cfg.RoomEnabled,
					"source":         cfg.SourceFile,
				},
				"boundProjects": available,
			}
			return env.printer().Print(RenderData{Brief: cfg.ProjectID, Prompt: prompt, JSON: jsonOut})
		},
	}

	cmd.AddCommand(bindCmd, useCmd, infoCmd)
	return cmd
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
		ProjectID:      projectID,
		ProjectName:    mapString(payload, "name"),
		OpenVikingRoot: mapString(payload, "openvikingRoot"),
		RoomEnabled:    mapString(payload, "roomId") != "",
	}, nil
}

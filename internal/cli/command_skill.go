package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

func newSkillCommand(env *CommandEnv) *cobra.Command {
	cmd := &cobra.Command{Use: "skill", Short: "Skill cache commands"}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List project skills",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := env.resolveProjectConfig(true)
			if err != nil {
				return err
			}
			client, _, err := env.clientWithToken(true)
			if err != nil {
				return err
			}
			ctx, cancel := env.context()
			defer cancel()
			payload, err := client.GetREST(ctx, "/api/projects/"+cfg.ProjectID+"/skills", nil)
			if err != nil {
				return err
			}
			return env.printer().Print(RenderData{Brief: fmt.Sprintf("%d skills", len(asSlice(payload["items"]))), Prompt: renderSkillListPrompt(payload), JSON: payload})
		},
	}

	showCmd := &cobra.Command{
		Use:   "show <skill_name>",
		Short: "Show skill markdown",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cfg, err := env.resolveProjectConfig(true)
			if err != nil {
				return err
			}
			client, _, err := env.clientWithToken(true)
			if err != nil {
				return err
			}
			ctx, cancel := env.context()
			defer cancel()
			payload, err := client.GetREST(ctx, "/api/projects/"+cfg.ProjectID+"/skills/"+strings.TrimSpace(args[0]), nil)
			if err != nil {
				return err
			}
			prompt := fmt.Sprintf("# Skill: %s\n\nURI: `%s`\n\n%s", mapString(payload, "name"), mapString(payload, "uri"), fallback(mapString(payload, "content"), "(empty)"))
			return env.printer().Print(RenderData{Brief: mapString(payload, "name"), Prompt: prompt, JSON: payload})
		},
	}

	pullCmd := &cobra.Command{
		Use:   "pull",
		Short: "Pull skills to .aitask/skills",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := env.resolveProjectConfig(true)
			if err != nil {
				return err
			}
			client, _, err := env.clientWithToken(true)
			if err != nil {
				return err
			}
			ctx, cancel := env.context()
			defer cancel()
			listPayload, err := client.GetREST(ctx, "/api/projects/"+cfg.ProjectID+"/skills", nil)
			if err != nil {
				return err
			}
			items := asSlice(listPayload["items"])
			written := []string{}
			for _, item := range items {
				entry := asMap(item)
				name := mapString(entry, "name")
				if name == "" {
					continue
				}
				contentPayload, err := client.GetREST(ctx, "/api/projects/"+cfg.ProjectID+"/skills/"+name, nil)
				if err != nil {
					return err
				}
				path := filepath.Join(cfg.AITaskDir, "skills", name+".md")
				if err := writeTextFile(path, fallback(mapString(contentPayload, "content"), "")); err != nil {
					return err
				}
				written = append(written, path)
			}
			prompt := "# Skills Pulled\n\n"
			if len(written) == 0 {
				prompt += "No skills returned by backend."
			} else {
				for _, item := range written {
					prompt += "- " + item + "\n"
				}
			}
			jsonOut := map[string]any{"count": len(written), "files": written}
			return env.printer().Print(RenderData{Brief: fmt.Sprintf("%d skills pulled", len(written)), Prompt: prompt, JSON: jsonOut})
		},
	}

	cmd.AddCommand(listCmd, showCmd, pullCmd)
	return cmd
}

func renderSkillListPrompt(payload map[string]any) string {
	items := asSlice(payload["items"])
	lines := []string{"# Skills", ""}
	if len(items) == 0 {
		lines = append(lines, "(empty)")
		return strings.Join(lines, "\n")
	}
	for _, item := range items {
		entry := asMap(item)
		lines = append(lines, fmt.Sprintf("- %s (%s)", mapString(entry, "name"), mapString(entry, "uri")))
	}
	return strings.Join(lines, "\n")
}

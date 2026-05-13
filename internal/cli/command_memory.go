package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

const maxPromptContentBytes = 4096

func newMemoryCommand(env *CommandEnv) *cobra.Command {
	cmd := &cobra.Command{Use: "memory", Short: "OpenViking memory operations"}

	searchCmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search memory refs/content",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := env.resolveProjectConfig(true)
			if err != nil {
				return err
			}
			budget, _ := cmd.Flags().GetInt("budget")
			refsOnly, _ := cmd.Flags().GetBool("refs-only")
			client, _, err := env.clientWithToken(true)
			if err != nil {
				return err
			}
			ctx, cancel := env.context()
			defer cancel()
			query := map[string]string{"q": strings.TrimSpace(args[0])}
			if budget > 0 {
				query["budget"] = fmt.Sprintf("%d", budget)
			}
			if refsOnly {
				query["refsOnly"] = "true"
			}
			payload, err := client.GetREST(ctx, "/api/projects/"+cfg.ProjectID+"/memory/search", query)
			if err != nil {
				return err
			}
			return env.printer().Print(RenderData{Brief: fmt.Sprintf("%d results", len(asSlice(payload["items"]))), Prompt: renderMemorySearchPrompt(payload), JSON: payload})
		},
	}
	searchCmd.Flags().Int("budget", 0, "token budget")
	searchCmd.Flags().Bool("refs-only", false, "return refs only")

	readCmd := &cobra.Command{
		Use:   "read <uri>",
		Short: "Read memory content by URI",
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
			payload, err := client.GetREST(ctx, "/api/projects/"+cfg.ProjectID+"/memory/read", map[string]string{"uri": strings.TrimSpace(args[0])})
			if err != nil {
				return err
			}
			prompt := renderMemoryReadPrompt(payload)
			return env.printer().Print(RenderData{Brief: mapString(payload, "uri"), Prompt: prompt, JSON: payload})
		},
	}

	writeCmd := &cobra.Command{
		Use:   "write",
		Short: "Write memory markdown",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := env.resolveProjectConfig(true)
			if err != nil {
				return err
			}
			from, _ := cmd.Flags().GetString("from")
			target, _ := cmd.Flags().GetString("target")
			title, _ := cmd.Flags().GetString("title")
			relatedTaskID, _ := cmd.Flags().GetString("task")
			autoSync, _ := cmd.Flags().GetBool("auto-sync")
			if strings.TrimSpace(from) == "" {
				return fmt.Errorf("--from is required")
			}
			if strings.TrimSpace(target) == "" {
				return fmt.Errorf("--target is required")
			}
			content, err := readTextFile(strings.TrimSpace(from))
			if err != nil {
				return err
			}
			if strings.TrimSpace(title) == "" {
				title = filepathBaseWithoutExt(from)
			}
			client, _, err := env.clientWithToken(true)
			if err != nil {
				return err
			}
			ctx, cancel := env.context()
			defer cancel()
			payload, err := client.PostREST(ctx, "/api/projects/"+cfg.ProjectID+"/memory/write", map[string]any{"target": target, "title": title, "content": content, "relatedTaskId": emptyAsNil(relatedTaskID), "autoSync": autoSync})
			if err != nil {
				return err
			}
			prompt := fmt.Sprintf("# Memory Written\n\nTarget: `%s`\nURI: `%s`", target, mapString(payload, "uri"))
			return env.printer().Print(RenderData{Brief: "memory written", Prompt: prompt, JSON: payload})
		},
	}
	writeCmd.Flags().String("from", "", "markdown file")
	writeCmd.Flags().String("target", "", "memory target (decisions/summary/resources)")
	writeCmd.Flags().String("title", "", "memory title")
	writeCmd.Flags().String("task", "", "related task id")
	writeCmd.Flags().Bool("auto-sync", false, "allow OpenViking to index the write asynchronously")

	cmd.AddCommand(searchCmd, readCmd, writeCmd)
	return cmd
}

func renderMemorySearchPrompt(payload map[string]any) string {
	items := asSlice(payload["items"])
	lines := []string{"# Memory Search", ""}
	if len(items) == 0 {
		lines = append(lines, "(empty)")
		return strings.Join(lines, "\n")
	}
	for _, item := range items {
		entry := asMap(item)
		line := fmt.Sprintf("- %s | %s", mapString(entry, "uri"), mapString(entry, "title"))
		if content := strings.TrimSpace(mapString(entry, "content")); content != "" {
			line += "\n  " + truncate(content, 180)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "..."
}

func renderMemoryReadPrompt(payload map[string]any) string {
	uri := mapString(payload, "uri")
	title := mapString(payload, "title")
	content := fallback(mapString(payload, "content"), "(empty)")
	if len(content) <= maxPromptContentBytes {
		return fmt.Sprintf("# Memory Read\n\nURI: `%s`\nTitle: %s\n\n%s", uri, title, content)
	}
	return fmt.Sprintf("# Memory Read (Refs)\n\nURI: `%s`\nTitle: %s\n\nContent is large (%d bytes). Use refs-first workflow:\n- `aitask memory search \"keyword\" --refs-only`\n- `aitask memory read %s`", uri, title, len(content), uri)
}

func filepathBaseWithoutExt(path string) string {
	base := path
	if idx := strings.LastIndexAny(base, "/\\"); idx >= 0 {
		base = base[idx+1:]
	}
	if idx := strings.LastIndex(base, "."); idx > 0 {
		base = base[:idx]
	}
	return strings.TrimSpace(base)
}

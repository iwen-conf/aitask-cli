package cli

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

func newSearchCommand(env *CommandEnv) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search OpenViking memory",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := env.resolveProjectConfig(true)
			if err != nil {
				return err
			}
			client, _, err := env.clientWithToken(true)
			if err != nil {
				return err
			}
			budget, _ := cmd.Flags().GetInt("budget")
			refsOnly, _ := cmd.Flags().GetBool("refs-only")
			query := map[string]string{"q": strings.TrimSpace(args[0])}
			if budget > 0 {
				query["budget"] = fmt.Sprintf("%d", budget)
			}
			if refsOnly {
				query["refsOnly"] = "true"
			}
			ctx, cancel := env.context()
			defer cancel()
			payload, err := client.GetREST(ctx, "/api/projects/"+cfg.ProjectID+"/memory/search", query)
			if err != nil || len(asSlice(payload["items"])) == 0 {
				fallbackPayload, fallbackErr := localSearchFallback(ctx, cfg.RootDir, args[0], refsOnly)
				if fallbackErr == nil && len(asSlice(fallbackPayload["items"])) > 0 {
					return env.printer().Print(RenderData{
						Brief:  fmt.Sprintf("%d fallback results", len(asSlice(fallbackPayload["items"]))),
						Prompt: renderMemorySearchPrompt(fallbackPayload),
						JSON:   fallbackPayload,
					})
				}
				if err != nil {
					return err
				}
			}
			return env.printer().Print(RenderData{
				Brief:  fmt.Sprintf("%d results", len(asSlice(payload["items"]))),
				Prompt: renderMemorySearchPrompt(payload),
				JSON:   payload,
			})
		},
	}
	cmd.Flags().Int("budget", 0, "token budget")
	cmd.Flags().Bool("refs-only", false, "return refs only")
	return cmd
}

func localSearchFallback(ctx context.Context, rootDir string, rawQuery string, refsOnly bool) (map[string]any, error) {
	rootDir = strings.TrimSpace(rootDir)
	if rootDir == "" {
		rootDir = "."
	}
	query := strings.TrimSpace(rawQuery)
	if query == "" {
		return map[string]any{"items": []any{}, "source": "local-rg"}, nil
	}
	rg, err := exec.LookPath("rg")
	if err != nil {
		return map[string]any{"items": []any{}, "source": "local-rg", "error": "rg not found"}, nil
	}
	cmd := exec.CommandContext(ctx, rg, "--fixed-strings", "--ignore-case", "--line-number", "--column", "--max-count", "3", "--glob", "!**/.git/**", "--glob", "!**/node_modules/**", "--glob", "!**/.aitask/state/**", "--", query, rootDir)
	raw, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return map[string]any{"items": []any{}, "source": "local-rg"}, nil
		}
		return nil, err
	}
	items := []any{}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	for _, line := range lines {
		item := parseRGLine(rootDir, line, refsOnly)
		if len(item) > 0 {
			items = append(items, item)
		}
		if len(items) >= 20 {
			break
		}
	}
	return map[string]any{"items": items, "source": "local-rg", "fallback": true}, nil
}

func parseRGLine(rootDir string, line string, refsOnly bool) map[string]any {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}
	parts := strings.SplitN(line, ":", 4)
	if len(parts) < 4 {
		return nil
	}
	path := strings.TrimPrefix(parts[0], strings.TrimRight(rootDir, "/")+"/")
	title := path + ":" + parts[1]
	item := map[string]any{
		"uri":   "file://" + path + ":" + parts[1],
		"title": title,
	}
	if !refsOnly {
		item["content"] = strings.TrimSpace(parts[3])
		item["snippet"] = strings.TrimSpace(parts[3])
	}
	return item
}

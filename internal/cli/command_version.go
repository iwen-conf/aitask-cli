package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const versionCheckWarnDays = 21

func newVersionCommand(env *CommandEnv) *cobra.Command {
	var check bool
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Show CLI version",
		RunE: func(_ *cobra.Command, _ []string) error {
			version := strings.TrimSpace(env.app.Version)
			if version == "" {
				version = "dev"
			}
			jsonOut := map[string]any{
				"name":    "aitask",
				"version": version,
			}
			prompt := "# AITask Version\n\n- Name: `aitask`\n- Version: `" + version + "`"
			if check {
				lastSyncAt, _ := readLastSyncTime(env)
				days := daysSince(lastSyncAt)
				needsUpdate := days >= versionCheckWarnDays
				jsonOut["check"] = map[string]any{
					"lastSyncAt":       lastSyncAt,
					"daysSinceSync":    days,
					"warnAfterDays":    versionCheckWarnDays,
					"upgradeSuggested": needsUpdate,
				}
				if needsUpdate {
					prompt += fmt.Sprintf("\n\n检查结果：距离最近一次同步已 `%d` 天，建议升级客户端。", days)
				} else {
					prompt += fmt.Sprintf("\n\n检查结果：最近同步 `%d` 天内，无需升级。", days)
				}
			}
			return env.printer().Print(RenderData{
				Brief:  version,
				Prompt: prompt,
				JSON:   jsonOut,
			})
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "check whether upgrade is recommended")
	return cmd
}

func readLastSyncTime(env *CommandEnv) (string, error) {
	cfg, err := env.resolveProjectConfig(false)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(cfg.AITaskDir) == "" {
		return "", fmt.Errorf("local project not initialized")
	}
	cache, err := readStateJSON(cfg.AITaskDir, stateLastSyncPB)
	if err != nil {
		return "", err
	}
	return mapString(cache, "syncAt"), nil
}

func daysSince(ts string) int {
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(ts))
	if err != nil {
		return 9999
	}
	if parsed.After(time.Now().UTC()) {
		return 0
	}
	return int(time.Since(parsed).Hours() / 24)
}

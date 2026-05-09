package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func newWhoAmICommand(env *CommandEnv) *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Show current agent identity",
		RunE: func(_ *cobra.Command, _ []string) error {
			client, _, err := env.clientWithToken(true)
			if err != nil {
				return err
			}
			ctx, cancel := env.context()
			defer cancel()
			res, err := client.WhoAmI(ctx)
			if err != nil {
				return err
			}
			identity := res.GetIdentity()
			profile := env.opts.profile
			if profile == "" {
				profile = DefaultProfileName
			}
			prompt := fmt.Sprintf("# Agent Identity\n\n- Profile: `%s`\n- Agent ID: `%s`\n- Type: `%s`\n- Role: `%s`\n- Scopes: %s\n- Allowed Projects: %s", profile, identity.GetAgentId(), identity.GetAgentType(), identity.GetRole(), strings.Join(identity.GetScopes(), ", "), strings.Join(identity.GetAllowedProjects(), ", "))
			brief := fmt.Sprintf("%s (%s) [profile=%s]", identity.GetAgentId(), identity.GetRole(), profile)
			jsonOut := map[string]any{
				"profile": profile,
				"identity": map[string]any{
					"agentId":         identity.GetAgentId(),
					"agentType":       identity.GetAgentType(),
					"role":            identity.GetRole(),
					"scopes":          identity.GetScopes(),
					"allowedProjects": identity.GetAllowedProjects(),
				},
			}
			return env.printer().Print(RenderData{Brief: brief, Prompt: prompt, JSON: jsonOut, Proto: res})
		},
	}
}

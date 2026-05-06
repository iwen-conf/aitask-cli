package cli

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

type bindCodeEnvelope struct {
	AgentID string `json:"agentId"`
	Token   string `json:"token"`
}

func newAuthCommand(env *CommandEnv) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage local agent token",
	}

	bind := &cobra.Command{
		Use:   "bind",
		Short: "Bind token from one-time code",
		RunE: func(cmd *cobra.Command, _ []string) error {
			code, _ := cmd.Flags().GetString("code")
			token, err := parseBindCode(code)
			if err != nil {
				return err
			}
			return importAndVerifyToken(env, token)
		},
	}
	bind.Flags().String("code", "", "one-time binding code")
	_ = bind.MarkFlagRequired("code")

	var inlineToken string
	importCmd := &cobra.Command{
		Use:   "import",
		Short: "Import token manually",
		RunE: func(_ *cobra.Command, _ []string) error {
			token := strings.TrimSpace(inlineToken)
			if token == "" {
				fmt.Fprint(env.app.Stderr, "Paste agent token: ")
				reader := bufio.NewReader(env.app.Stdin)
				raw, err := reader.ReadString('\n')
				if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, os.ErrClosed) {
					return err
				}
				token = strings.TrimSpace(raw)
			}
			return importAndVerifyToken(env, token)
		},
	}
	importCmd.Flags().StringVar(&inlineToken, "token", "", "agent token")

	tokenCmd := &cobra.Command{Use: "token", Short: "Token commands"}
	tokenCmd.AddCommand(importCmd)

	cmd.AddCommand(bind, tokenCmd)
	return cmd
}

func importAndVerifyToken(env *CommandEnv, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return errors.New("token cannot be empty")
	}
	client := NewClient(env.opts.serverURL, env.opts.timeout, token)
	ctx, cancel := env.context()
	defer cancel()
	whoami, err := client.WhoAmI(ctx)
	if err != nil {
		return err
	}
	if err := env.tokenStore.Save(env.opts.serverURL, token); err != nil {
		return err
	}
	identity := whoami.GetIdentity()
	result := map[string]any{
		"stored": true,
		"identity": map[string]any{
			"agentId":         identity.GetAgentId(),
			"agentType":       identity.GetAgentType(),
			"role":            identity.GetRole(),
			"scopes":          identity.GetScopes(),
			"allowedProjects": identity.GetAllowedProjects(),
		},
	}
	prompt := fmt.Sprintf("# Auth Bound\n\nAgent: `%s` (%s)\nRole: `%s`\n\nToken has been stored in secure local storage.", identity.GetAgentId(), identity.GetAgentType(), identity.GetRole())
	return env.printer().Print(RenderData{Brief: "Token stored successfully", Prompt: prompt, JSON: result, Proto: whoami})
}

func parseBindCode(code string) (string, error) {
	trimmed := strings.TrimSpace(code)
	if trimmed == "" {
		return "", errors.New("code cannot be empty")
	}
	if strings.HasPrefix(trimmed, "aitask-bind:") {
		payload := strings.TrimPrefix(trimmed, "aitask-bind:")
		raw, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			raw, err = base64.RawStdEncoding.DecodeString(payload)
			if err != nil {
				return "", fmt.Errorf("invalid bind code payload: %w", err)
			}
		}
		var envelope bindCodeEnvelope
		if err := json.Unmarshal(raw, &envelope); err != nil {
			return "", err
		}
		if strings.TrimSpace(envelope.Token) == "" {
			return "", errors.New("bind code token is empty")
		}
		return strings.TrimSpace(envelope.Token), nil
	}
	if strings.Contains(trimmed, ":") {
		parts := strings.SplitN(trimmed, ":", 2)
		if strings.HasPrefix(parts[0], "agt_") && strings.TrimSpace(parts[1]) != "" {
			return strings.TrimSpace(parts[1]), nil
		}
	}
	return trimmed, nil
}

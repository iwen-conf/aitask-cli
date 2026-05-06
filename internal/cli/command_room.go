package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"github.com/spf13/cobra"
)

func newRoomCommand(env *CommandEnv) *cobra.Command {
	cmd := &cobra.Command{Use: "room", Short: "Project room commands"}

	joinCmd := &cobra.Command{
		Use:   "join",
		Short: "Join room and show snapshot",
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
			payload, err := client.GetREST(ctx, "/api/projects/"+cfg.ProjectID+"/room", nil)
			if err != nil {
				return err
			}
			_ = writeStateJSON(cfg.AITaskDir, stateRoomSnapshotPB, payload)
			safeWriteLastSync(cfg.AITaskDir, "room join", "online", false)
			prompt := fmt.Sprintf("# Room Joined\n\nRoom ID: `%s`\nStatus: `%s`\nMembers: %d", mapString(payload, "roomId"), mapString(payload, "status"), len(asSlice(payload["members"])))
			return env.printer().Print(RenderData{Brief: mapString(payload, "roomId"), Prompt: prompt, JSON: payload})
		},
	}

	sendCmd := &cobra.Command{
		Use:   "send <message>",
		Short: "Send room message",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := env.resolveProjectConfig(true)
			if err != nil {
				return err
			}
			messageType, _ := cmd.Flags().GetString("type")
			if strings.TrimSpace(messageType) == "" {
				messageType = "text"
			}
			client, _, err := env.clientWithToken(true)
			if err != nil {
				return err
			}
			ctx, cancel := env.context()
			defer cancel()
			payload, err := client.PostREST(ctx, "/api/projects/"+cfg.ProjectID+"/room/messages", map[string]any{"messageType": messageType, "content": strings.TrimSpace(args[0])})
			if err != nil {
				return err
			}
			prompt := fmt.Sprintf("# Room Message Sent\n\nMessage ID: `%s`", mapString(payload, "messageId"))
			return env.printer().Print(RenderData{Brief: "sent", Prompt: prompt, JSON: payload})
		},
	}
	sendCmd.Flags().String("type", "text", "message type")

	historyCmd := &cobra.Command{
		Use:   "history",
		Short: "Show room message or mention history",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := env.resolveProjectConfig(true)
			if err != nil {
				return err
			}
			mentions, _ := cmd.Flags().GetBool("mentions")
			limit, _ := cmd.Flags().GetInt("limit")
			client, _, err := env.clientWithToken(true)
			if err != nil {
				return err
			}
			ctx, cancel := env.context()
			defer cancel()
			if mentions {
				payload, err := client.GetREST(ctx, "/api/projects/"+cfg.ProjectID+"/room/mentions", nil)
				if err != nil {
					return err
				}
				return env.printer().Print(RenderData{Brief: fmt.Sprintf("%d mentions", len(asSlice(payload["items"]))), Prompt: renderRoomMentionsPrompt(payload), JSON: payload})
			}
			payload, err := client.GetREST(ctx, "/api/projects/"+cfg.ProjectID+"/room/messages", map[string]string{"limit": fmt.Sprintf("%d", limit)})
			if err != nil {
				return err
			}
			return env.printer().Print(RenderData{Brief: fmt.Sprintf("%d messages", len(asSlice(payload["items"]))), Prompt: renderRoomHistoryPrompt(payload), JSON: payload})
		},
	}
	historyCmd.Flags().Int("limit", 30, "message limit")
	historyCmd.Flags().Bool("mentions", false, "show mentions instead of messages")

	askCmd := &cobra.Command{
		Use:   "ask <agent_type> <question>",
		Short: "Ask a target agent via room mention",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			cfg, err := env.resolveProjectConfig(true)
			if err != nil {
				return err
			}
			content := "@" + strings.TrimSpace(args[0]) + " " + strings.TrimSpace(args[1])
			client, _, err := env.clientWithToken(true)
			if err != nil {
				return err
			}
			ctx, cancel := env.context()
			defer cancel()
			payload, err := client.PostREST(ctx, "/api/projects/"+cfg.ProjectID+"/room/messages", map[string]any{"messageType": "question", "content": content})
			if err != nil {
				return err
			}
			prompt := fmt.Sprintf("# Room Ask Sent\n\nMessage ID: `%s`\nMention: `%s`", mapString(payload, "messageId"), args[0])
			return env.printer().Print(RenderData{Brief: "ask sent", Prompt: prompt, JSON: payload})
		},
	}

	pinCmd := &cobra.Command{
		Use:   "pin <message_id>",
		Short: "Pin a room message",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := env.resolveProjectConfig(true)
			if err != nil {
				return err
			}
			asValue, _ := cmd.Flags().GetString("as")
			if strings.TrimSpace(asValue) == "" {
				asValue = "decision"
			}
			client, _, err := env.clientWithToken(true)
			if err != nil {
				return err
			}
			ctx, cancel := env.context()
			defer cancel()
			payload, err := client.PostREST(ctx, "/api/projects/"+cfg.ProjectID+"/room/messages/"+strings.TrimSpace(args[0])+"/pin", map[string]any{"as": asValue})
			if err != nil {
				return err
			}
			prompt := fmt.Sprintf("# Room Message Pinned\n\nMessage `%s` pinned as `%s`.", args[0], asValue)
			return env.printer().Print(RenderData{Brief: "pinned", Prompt: prompt, JSON: payload})
		},
	}
	pinCmd.Flags().String("as", "decision", "pin category")

	watchCmd := &cobra.Command{
		Use:   "watch",
		Short: "Watch room websocket events",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := env.resolveProjectConfig(true)
			if err != nil {
				return err
			}
			client, token, err := env.clientWithToken(true)
			if err != nil {
				return err
			}
			wsURL, err := client.WebSocketURL(cfg.ProjectID)
			if err != nil {
				return err
			}
			header := map[string][]string{}
			if strings.TrimSpace(token) != "" {
				header["Authorization"] = []string{"Bearer " + token}
			}
			conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
			if err != nil {
				return err
			}
			defer conn.Close()

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			done := make(chan error, 1)
			go func() {
				for {
					_, msg, err := conn.ReadMessage()
					if err != nil {
						done <- err
						return
					}
					var envelope map[string]any
					if err := json.Unmarshal(msg, &envelope); err == nil {
						if payload, err := json.Marshal(envelope); err == nil {
							fmt.Fprintln(env.app.Stdout, string(payload))
						}
						continue
					}
					fmt.Fprintln(env.app.Stdout, string(msg))
				}
			}()

			pingTicker := time.NewTicker(20 * time.Second)
			defer pingTicker.Stop()

			for {
				select {
				case <-ctx.Done():
					return nil
				case err := <-done:
					if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
						return nil
					}
					return err
				case <-pingTicker.C:
					_ = conn.WriteJSON(map[string]any{"type": "ping", "sentAt": time.Now().UTC().Format(time.RFC3339)})
				}
			}
		},
	}

	cmd.AddCommand(joinCmd, sendCmd, watchCmd, historyCmd, askCmd, pinCmd)
	return cmd
}

func renderRoomHistoryPrompt(payload map[string]any) string {
	items := asSlice(payload["items"])
	lines := []string{"# Room History", ""}
	if len(items) == 0 {
		lines = append(lines, "(empty)")
		return strings.Join(lines, "\n")
	}
	for _, item := range items {
		entry := asMap(item)
		sender := asMap(entry["sender"])
		who := mapString(sender, "agentType")
		if who == "" {
			who = mapString(sender, "operatorLabel")
		}
		if who == "" {
			who = mapString(sender, "type")
		}
		lines = append(lines, fmt.Sprintf("- [%s] %s: %s", mapString(entry, "messageType"), who, mapString(entry, "content")))
	}
	return strings.Join(lines, "\n")
}

func renderRoomMentionsPrompt(payload map[string]any) string {
	items := asSlice(payload["items"])
	lines := []string{"# Room Mentions", ""}
	if len(items) == 0 {
		lines = append(lines, "(empty)")
		return strings.Join(lines, "\n")
	}
	for _, item := range items {
		entry := asMap(item)
		target := mapString(entry, "mentionedAgentId")
		if target == "" {
			target = mapString(entry, "mentionedAgentType")
		}
		lines = append(lines, fmt.Sprintf("- %s -> handled=%t (message=%s)", target, mapBool(entry, "handled"), mapString(entry, "messageId")))
	}
	return strings.Join(lines, "\n")
}

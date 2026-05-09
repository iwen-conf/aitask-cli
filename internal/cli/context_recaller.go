package cli

import (
	"context"
	"fmt"
	"strings"
)

type backendRecaller struct {
	client    *Client
	projectID string
}

func (r *backendRecaller) Recall(ctx context.Context, agent, eventID string) (string, error) {
	if r == nil || r.client == nil || strings.TrimSpace(r.projectID) == "" || strings.TrimSpace(eventID) == "" {
		return "", nil
	}
	eventID = strings.TrimSpace(eventID)
	payload, err := r.client.GetREST(ctx, "/api/projects/"+strings.TrimSpace(r.projectID)+"/memory/search", map[string]string{
		"q":      "evt:" + eventID + " event:" + eventID + " " + eventID,
		"budget": "2048",
	})
	if err != nil {
		return "", nil
	}
	return renderRecallPayload(payload), nil
}

func renderRecallPayload(payload map[string]any) string {
	items := asSlice(payload["items"])
	if len(items) == 0 {
		return strings.TrimSpace(fallback(mapString(payload, "content"), mapString(payload, "summary")))
	}
	lines := make([]string, 0, len(items))
	for _, item := range items {
		entry := asMap(item)
		uri := mapString(entry, "uri")
		title := mapString(entry, "title")
		content := fallback(mapString(entry, "content"), mapString(entry, "summary"))
		line := strings.TrimSpace(fmt.Sprintf("%s %s", uri, title))
		if line == "" {
			line = "memory result"
		}
		if strings.TrimSpace(content) != "" {
			line += "\n" + truncate(content, 800)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n\n")
}

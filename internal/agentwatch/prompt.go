package agentwatch

import (
	"fmt"
	"strings"
)

type PromptEvent struct {
	ID        string
	Kind      string
	From      string
	Project   string
	ThreadID  string
	CreatedAt string
	Body      string
}

func RenderPrompt(eventID, agent, recall string, evt PromptEvent) string {
	eventID = firstNonEmpty(eventID, evt.ID)
	recall = strings.TrimSpace(recall)
	if recall == "" {
		recall = "(none)"
	}
	body := strings.TrimSpace(evt.Body)
	if body == "" {
		body = "(empty)"
	}
	return fmt.Sprintf(`You are %s in an AITask multi-agent collaboration system.

You received an actionable event.

Event:
- ID: %s
- Kind: %s
- From: %s
- To: %s
- Project: %s
- Thread: %s
- Created At: %s

Task:
%s

Relevant Context:
%s

Instructions:
1. Decide whether this event is addressed to you.
2. If actionable, complete the task.
3. Reply through AITask.
4. Mark the event done, skipped, or failed.
5. Do not process events sent by yourself.`,
		strings.TrimSpace(agent),
		valueOrUnknown(eventID),
		valueOrUnknown(evt.Kind),
		valueOrUnknown(evt.From),
		valueOrUnknown(agent),
		valueOrUnknown(evt.Project),
		valueOrUnknown(evt.ThreadID),
		valueOrUnknown(evt.CreatedAt),
		body,
		recall,
	)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func valueOrUnknown(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "(unknown)"
	}
	return value
}

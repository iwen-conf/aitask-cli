package cli

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseMentionTokens(t *testing.T) {
	got := parseMentionTokens("@codex please pair with @agt_123 and @codex; ignore @x")
	want := []string{"codex", "agt_123"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tokens = %#v, want %#v", got, want)
	}
}

func TestMatchingMentionTokens(t *testing.T) {
	identity := eventsIdentity{AgentID: "agt_me", AgentType: "codex", OperatorLabel: "local-console"}
	got := matchingMentionTokens([]string{"gemini", "codex", "agt_me", "local-console"}, identity)
	want := []string{"codex", "agt_me", "local-console"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("matches = %#v, want %#v", got, want)
	}
}

func TestEventFromEnvelopeFilters(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	identity := eventsIdentity{AgentID: "agt_me", AgentType: "codex"}
	filters := eventsFilterSet{eventKindMention: true, eventKindTaskDelegated: true, eventKindTaskUpdated: true}

	tests := []struct {
		name        string
		envelope    roomEventEnvelope
		includeSelf bool
		wantKind    string
		wantOK      bool
	}{
		{
			name: "mention to type",
			envelope: roomEventEnvelope{
				EventID:   "evt_mention",
				EventType: "room.message",
				ProjectID: "prj_1",
				Sender:    eventsSender{Type: "agent", AgentID: stringPtr("agt_other"), AgentType: stringPtr("claude")},
				Payload:   map[string]any{"messageId": "msg_1", "content": "@codex please handle"},
				CreatedAt: now,
			},
			wantKind: eventKindMention,
			wantOK:   true,
		},
		{
			name: "self mention skipped",
			envelope: roomEventEnvelope{
				EventID:   "evt_self",
				EventType: "room.message",
				ProjectID: "prj_1",
				Sender:    eventsSender{Type: "agent", AgentID: stringPtr("agt_me"), AgentType: stringPtr("codex")},
				Payload:   map[string]any{"messageId": "msg_2", "content": "@codex note to self"},
				CreatedAt: now,
			},
			wantOK: false,
		},
		{
			name: "self mention included",
			envelope: roomEventEnvelope{
				EventID:   "evt_self_included",
				EventType: "room.message",
				ProjectID: "prj_1",
				Sender:    eventsSender{Type: "agent", AgentID: stringPtr("agt_me"), AgentType: stringPtr("codex")},
				Payload:   map[string]any{"messageId": "msg_3", "content": "@codex note to self"},
				CreatedAt: now,
			},
			includeSelf: true,
			wantKind:    eventKindMention,
			wantOK:      true,
		},
		{
			name: "delegated task assigned to us",
			envelope: roomEventEnvelope{
				EventID:   "evt_task",
				EventType: "task.updated",
				ProjectID: "prj_1",
				Payload: map[string]any{
					"taskId":     "task_1",
					"eventType":  "task.delegated",
					"fromStatus": "planned",
					"toStatus":   "delegated",
					"details":    map[string]any{"assigneeAgentId": "agt_me"},
				},
				CreatedAt: now,
			},
			wantKind: eventKindTaskDelegated,
			wantOK:   true,
		},
		{
			name: "task update assigned to us",
			envelope: roomEventEnvelope{
				EventID:   "evt_task_update",
				EventType: "task.updated",
				ProjectID: "prj_1",
				Payload: map[string]any{
					"taskId":     "task_1",
					"eventType":  "task.started",
					"fromStatus": "delegated",
					"toStatus":   "running",
					"details":    map[string]any{"assigneeAgentId": "agt_me"},
				},
				CreatedAt: now,
			},
			wantKind: eventKindTaskUpdated,
			wantOK:   true,
		},
		{
			name: "delegated task assigned elsewhere",
			envelope: roomEventEnvelope{
				EventID:   "evt_task_other",
				EventType: "task.updated",
				ProjectID: "prj_1",
				Payload: map[string]any{
					"taskId":    "task_2",
					"eventType": "task.delegated",
					"details":   map[string]any{"assigneeAgentId": "agt_other"},
				},
				CreatedAt: now,
			},
			wantOK: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := eventFromEnvelope(tc.envelope, identity, filters, tc.includeSelf)
			if ok != tc.wantOK {
				t.Fatalf("ok = %t, want %t; event=%#v", ok, tc.wantOK, got)
			}
			if ok && got.Kind != tc.wantKind {
				t.Fatalf("kind = %q, want %q", got.Kind, tc.wantKind)
			}
		})
	}
}

func TestEventFromEnvelopeSystemAndRoomFilters(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	envelope := roomEventEnvelope{
		EventID:   "evt_system",
		EventType: "room.message",
		ProjectID: "prj_1",
		Sender:    eventsSender{Type: "system"},
		Payload: map[string]any{
			"messageId":   "msg_system",
			"messageType": "system_event",
			"content":     "Task updated",
			"payload":     map[string]any{"eventType": "task.started"},
		},
		CreatedAt: now,
	}
	got, ok := eventFromEnvelope(envelope, eventsIdentity{AgentID: "agt_me", AgentType: "codex"}, eventsFilterSet{eventKindSystemEvent: true}, false)
	if !ok || got.Kind != eventKindSystemEvent {
		t.Fatalf("system filter produced (%#v, %t)", got, ok)
	}
	got, ok = eventFromEnvelope(envelope, eventsIdentity{AgentID: "agt_me", AgentType: "codex"}, eventsFilterSet{eventKindRoomMessage: true}, false)
	if !ok || got.Kind != eventKindRoomMessage {
		t.Fatalf("room_message filter produced (%#v, %t)", got, ok)
	}
}

func TestSerializeEventStableShape(t *testing.T) {
	catchup := true
	event := eventsOutput{
		Kind:            eventKindTaskDelegated,
		TS:              "2026-05-07T12:00:00Z",
		Project:         "prj_1",
		EventID:         "evt_1",
		TaskID:          "task_1",
		AssigneeAgentID: "agt_me",
		FromStatus:      "planned",
		ToStatus:        "delegated",
		Details:         map[string]any{"reason": "test"},
		Catchup:         &catchup,
	}
	got, err := serializeEvent(event)
	if err != nil {
		t.Fatalf("serializeEvent: %v", err)
	}
	want := `{"kind":"task_delegated","ts":"2026-05-07T12:00:00Z","project":"prj_1","eventId":"evt_1","taskId":"task_1","assigneeAgentId":"agt_me","fromStatus":"planned","toStatus":"delegated","details":{"reason":"test"},"catchup":true}`
	if string(got) != want {
		t.Fatalf("json = %s\nwant = %s", got, want)
	}
	var decoded map[string]any
	if err := json.Unmarshal(got, &decoded); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
}

func TestCatchupMentionEventTargets(t *testing.T) {
	item := map[string]any{
		"mentionId":          "mention_1",
		"messageId":          "msg_1",
		"mentionedAgentType": "codex",
		"handled":            false,
		"createdAt":          "2026-05-07T12:00:00Z",
	}
	event := catchupMentionEvent("prj_1", item, eventsIdentity{AgentID: "agt_me", AgentType: "codex"})
	if event.Kind != eventKindMention || event.EventID != "catchup:mention:mention_1" {
		t.Fatalf("unexpected catchup event: %#v", event)
	}
	if !reflect.DeepEqual(event.Mentions, []string{"codex"}) {
		t.Fatalf("mentions = %#v", event.Mentions)
	}
	if event.Catchup == nil || !*event.Catchup {
		t.Fatalf("catchup flag missing: %#v", event.Catchup)
	}
}

func TestEventsReconnectDelay(t *testing.T) {
	base := time.Second
	max := 30 * time.Second
	tests := []struct {
		name    string
		attempt int
		jitter  float64
		want    time.Duration
	}{
		{name: "first no jitter", attempt: 1, want: time.Second},
		{name: "second positive jitter", attempt: 2, jitter: 1, want: 2400 * time.Millisecond},
		{name: "third negative jitter", attempt: 3, jitter: -1, want: 3200 * time.Millisecond},
		{name: "capped before jitter", attempt: 8, jitter: 1, want: 30 * time.Second},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := eventsReconnectDelay(tc.attempt, base, max, func() float64 { return tc.jitter })
			if got != tc.want {
				t.Fatalf("delay = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestWaitEventsReadyTracksConnectedProjects(t *testing.T) {
	readyCh := make(chan eventsReadyState, 2)
	readyCh <- eventsReadyState{projectID: "prj_1", connected: true}
	readyCh <- eventsReadyState{projectID: "prj_2"}
	connected, err := waitEventsReady(t.Context(), readyCh, 2, time.Second)
	if err != nil {
		t.Fatalf("waitEventsReady: %v", err)
	}
	if _, ok := connected["prj_1"]; !ok || len(connected) != 1 {
		t.Fatalf("connected = %#v", connected)
	}
}

func TestWaitAnyEventsProjectConnectedTimeout(t *testing.T) {
	err := waitAnyEventsProjectConnected(t.Context(), make(chan string), make(chan struct{}), time.Nanosecond)
	if err == nil {
		t.Fatalf("expected timeout")
	}
}

func TestParseEventsNotifyKinds(t *testing.T) {
	got, err := parseEventsNotifyKinds([]string{"mention,task_delegated", "mention"})
	if err != nil {
		t.Fatalf("parseEventsNotifyKinds: %v", err)
	}
	want := []string{eventKindMention, eventKindTaskDelegated}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("notify kinds = %#v, want %#v", got, want)
	}
	if _, err := parseEventsNotifyKinds([]string{"all"}); err == nil {
		t.Fatalf("expected all to be rejected for notify kinds")
	}
}

func TestParseEventsSize(t *testing.T) {
	got, err := parseEventsSize("1.5MiB")
	if err != nil {
		t.Fatalf("parseEventsSize: %v", err)
	}
	if got != 1572864 {
		t.Fatalf("size = %d", got)
	}
}

func TestEventsFileSinkWritesAndRotates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.ndjson")
	sink := &eventsFileSink{path: path, maxSize: 16, maxBackups: 1, compress: false}
	if _, err := sink.Write([]byte("first line\n")); err != nil {
		t.Fatalf("write first: %v", err)
	}
	if _, err := sink.Write([]byte("second line\n")); err != nil {
		t.Fatalf("write second: %v", err)
	}
	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read current: %v", err)
	}
	rotated, err := os.ReadFile(path + ".1")
	if err != nil {
		t.Fatalf("read rotated: %v", err)
	}
	if string(current) != "second line\n" || string(rotated) != "first line\n" {
		t.Fatalf("current=%q rotated=%q", current, rotated)
	}
}

func TestEventsFileSinkCompressesRotatedBackups(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.ndjson")
	sink := &eventsFileSink{path: path, maxSize: 16, maxBackups: 1, compress: true}
	if _, err := sink.Write([]byte("first line\n")); err != nil {
		t.Fatalf("write first: %v", err)
	}
	if _, err := sink.Write([]byte("second line\n")); err != nil {
		t.Fatalf("write second: %v", err)
	}
	file, err := os.Open(path + ".1.gz")
	if err != nil {
		t.Fatalf("open rotated gzip: %v", err)
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer gz.Close()
	rotated, err := io.ReadAll(gz)
	if err != nil {
		t.Fatalf("read gzip: %v", err)
	}
	if string(rotated) != "first line\n" {
		t.Fatalf("rotated gzip = %q", rotated)
	}
}

func TestSummarizeEventNotification(t *testing.T) {
	from := eventsSender{Type: "agent", AgentType: stringPtr("claude")}
	title, body := summarizeEventNotification(eventsOutput{
		Kind:    eventKindMention,
		Project: "prj_0123456789abcdef",
		From:    &from,
		Content: "@codex please inspect",
	})
	if !strings.Contains(title, eventKindMention) || !strings.Contains(title, "prj_01234567") {
		t.Fatalf("title = %q", title)
	}
	if !strings.Contains(body, "claude: @codex please inspect") {
		t.Fatalf("body = %q", body)
	}
}

func TestParseEventsFilters(t *testing.T) {
	got, err := parseEventsFilters([]string{"mention,task_delegated", "system_event"})
	if err != nil {
		t.Fatalf("parseEventsFilters: %v", err)
	}
	want := []string{eventKindMention, eventKindSystemEvent, eventKindTaskDelegated}
	if !reflect.DeepEqual(sortedEventFilterKinds(got), want) {
		t.Fatalf("filters = %#v, want %#v", sortedEventFilterKinds(got), want)
	}
	got, err = parseEventsFilters([]string{"mention", "all"})
	if err != nil {
		t.Fatalf("parse all: %v", err)
	}
	if !got[eventKindAll] || len(got) != 1 {
		t.Fatalf("all should override other filters, got %#v", got)
	}
	if _, err := parseEventsFilters([]string{"unknown"}); err == nil {
		t.Fatalf("expected unsupported filter error")
	}
}

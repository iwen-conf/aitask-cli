package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	aitaskv1 "github.com/iwen-conf/aitask-cli/internal/rpc/gen/aitask/v1"
	"github.com/spf13/cobra"
)

const (
	eventKindAll           = "all"
	eventKindError         = "error"
	eventKindEvent         = "event"
	eventKindMention       = "mention"
	eventKindReady         = "ready"
	eventKindReconnect     = "reconnect"
	eventKindRoomMessage   = "room_message"
	eventKindShutdown      = "shutdown"
	eventKindSystemEvent   = "system_event"
	eventKindTaskDelegated = "task_delegated"
	eventKindTaskUpdated   = "task_updated"

	eventsStartupTimeout = 60 * time.Second
)

var (
	eventsMentionPattern  = regexp.MustCompile(`@([a-zA-Z0-9_-]{2,80})`)
	validEventsFilterKind = map[string]struct{}{
		eventKindAll:           {},
		eventKindMention:       {},
		eventKindTaskDelegated: {},
		eventKindTaskUpdated:   {},
		eventKindSystemEvent:   {},
		eventKindRoomMessage:   {},
	}
)

type eventsOptions struct {
	projects       []string
	filters        eventsFilterSet
	outputFile     string
	stdout         bool
	rotateSize     int64
	rotateBackups  int
	rotateCompress bool
	notify         string
	notifyKinds    []string
	noCatchup      bool
	includeSelf    bool
	pingInterval   time.Duration
	reconnectBase  time.Duration
	reconnectMax   time.Duration
}

type eventsFilterSet map[string]bool

func (s eventsFilterSet) allows(kind string) bool {
	return s[eventKindAll] || s[kind]
}

type eventsIdentity struct {
	AgentID         string
	AgentType       string
	OperatorLabel   string
	AllowedProjects []string
}

type eventsSender struct {
	Type          string  `json:"type"`
	AgentType     *string `json:"agentType"`
	AgentID       *string `json:"agentId"`
	OperatorLabel *string `json:"operatorLabel"`
}

type roomEventEnvelope struct {
	EventID   string         `json:"eventId"`
	EventType string         `json:"eventType"`
	ProjectID string         `json:"projectId"`
	RoomID    string         `json:"roomId"`
	Sender    eventsSender   `json:"sender"`
	Payload   map[string]any `json:"payload"`
	CreatedAt time.Time      `json:"createdAt"`
}

type eventsOutput struct {
	Kind            string         `json:"kind"`
	TS              string         `json:"ts"`
	Project         string         `json:"project,omitempty"`
	Projects        []string       `json:"projects,omitempty"`
	EventID         string         `json:"eventId,omitempty"`
	MessageID       string         `json:"messageId,omitempty"`
	TaskID          string         `json:"taskId,omitempty"`
	AssigneeAgentID string         `json:"assigneeAgentId,omitempty"`
	FromStatus      string         `json:"fromStatus,omitempty"`
	ToStatus        string         `json:"toStatus,omitempty"`
	From            *eventsSender  `json:"from,omitempty"`
	Content         string         `json:"content,omitempty"`
	Mentions        []string       `json:"mentions,omitempty"`
	Details         map[string]any `json:"details,omitempty"`
	Catchup         *bool          `json:"catchup,omitempty"`
	Attempt         int            `json:"attempt,omitempty"`
	Reason          string         `json:"reason,omitempty"`
	Fatal           *bool          `json:"fatal,omitempty"`
}

type eventsEmitter struct {
	stream   []io.Writer
	stderr   io.Writer
	closer   io.Closer
	notifier *eventsNotifier
	mu       sync.Mutex
}

type eventsDeduper struct {
	mu   sync.Mutex
	seen map[string]struct{}
}

type eventsReadyState struct {
	projectID string
	connected bool
}

type eventsCommandDefaults struct {
	outputFile     string
	stdout         bool
	rotateSizeRaw  string
	rotateBackups  int
	rotateCompress bool
	notify         string
	notifyKinds    []string
}

func newEventsCommand(env *CommandEnv) *cobra.Command {
	return newEventsCommandWithDefaults(env, eventsCommandDefaults{
		stdout:         true,
		rotateSizeRaw:  "10MiB",
		rotateBackups:  5,
		rotateCompress: true,
		notify:         "none",
		notifyKinds:    []string{eventKindMention, eventKindTaskDelegated},
	})
}

func newEventsDaemonCommand(env *CommandEnv) *cobra.Command {
	return newEventsCommandWithDefaults(env, eventsCommandDefaults{
		outputFile:     "~/.aitask/events.ndjson",
		stdout:         true,
		rotateSizeRaw:  "10MiB",
		rotateBackups:  5,
		rotateCompress: true,
		notify:         "none",
		notifyKinds:    []string{eventKindMention, eventKindTaskDelegated},
	})
}

func newEventsCommandWithDefaults(env *CommandEnv, defaults eventsCommandDefaults) *cobra.Command {
	var projectFlags []string
	var filterFlags []string
	var notifyKindFlags []string
	rotateSizeRaw := defaults.rotateSizeRaw
	opts := eventsOptions{
		outputFile:     defaults.outputFile,
		stdout:         defaults.stdout,
		rotateBackups:  defaults.rotateBackups,
		rotateCompress: defaults.rotateCompress,
		notify:         defaults.notify,
		pingInterval:   20 * time.Second,
		reconnectBase:  time.Second,
		reconnectMax:   30 * time.Second,
	}

	cmd := &cobra.Command{
		Use:   "events",
		Short: "Stream actionable project events as NDJSON",
		RunE: func(cmd *cobra.Command, _ []string) error {
			filters, err := parseEventsFilters(filterFlags)
			if err != nil {
				return err
			}
			opts.filters = filters
			notifyKinds, err := parseEventsNotifyKinds(notifyKindFlags)
			if err != nil {
				return err
			}
			opts.notifyKinds = notifyKinds
			rotateSize, err := parseEventsSize(rotateSizeRaw)
			if err != nil {
				return err
			}
			opts.rotateSize = rotateSize
			opts.projects = append([]string{}, projectFlags...)
			if strings.TrimSpace(env.opts.projectID) != "" {
				opts.projects = append(opts.projects, env.opts.projectID)
			}
			if err := validateEventsOptions(opts); err != nil {
				return err
			}
			return runEventsCommand(cmd.Context(), env, opts)
		},
	}

	cmd.Flags().StringArrayVar(&projectFlags, "project", nil, "project id to watch (repeatable)")
	cmd.Flags().StringArrayVar(&filterFlags, "filter", []string{eventKindMention + "," + eventKindTaskDelegated}, "event kind filter (repeatable or comma-separated)")
	cmd.Flags().StringVar(&opts.outputFile, "output-file", opts.outputFile, "NDJSON sink path; set empty to disable file sink")
	cmd.Flags().BoolVar(&opts.stdout, "stdout", opts.stdout, "emit NDJSON to stdout")
	cmd.Flags().StringVar(&rotateSizeRaw, "rotate-size", rotateSizeRaw, "file rotation threshold")
	cmd.Flags().IntVar(&opts.rotateBackups, "rotate-backups", opts.rotateBackups, "number of rotated archives to keep")
	cmd.Flags().BoolVar(&opts.rotateCompress, "rotate-compress", opts.rotateCompress, "gzip rotated archives")
	cmd.Flags().StringVar(&opts.notify, "notify", opts.notify, "notification backend: none|osascript|terminal-notifier|auto")
	cmd.Flags().StringArrayVar(&notifyKindFlags, "notify-kinds", defaults.notifyKinds, "event kinds that trigger notifications")
	cmd.Flags().BoolVar(&opts.noCatchup, "no-catchup", false, "skip startup REST catch-up")
	cmd.Flags().BoolVar(&opts.includeSelf, "include-self", false, "include events sent by the current agent")
	cmd.Flags().DurationVar(&opts.pingInterval, "ping-interval", 20*time.Second, "websocket ping interval")
	cmd.Flags().DurationVar(&opts.reconnectBase, "reconnect-base", time.Second, "initial reconnect backoff")
	cmd.Flags().DurationVar(&opts.reconnectMax, "reconnect-max", 30*time.Second, "maximum reconnect backoff")
	return cmd
}

func runEventsCommand(parent context.Context, env *CommandEnv, opts eventsOptions) error {
	emitter, err := newEventsEmitter(env, opts)
	if err != nil {
		return err
	}
	defer emitter.Close()
	client, token, err := env.clientWithToken(true)
	if err != nil {
		_ = emitter.emitStderr(eventsError("", true, err.Error()))
		return err
	}

	identity, err := loadEventsIdentity(env, client)
	if err != nil {
		_ = emitter.emitStderr(eventsError("", true, err.Error()))
		return err
	}

	projects := resolveEventsProjects(opts.projects, identity.AllowedProjects)
	if len(projects) == 0 {
		err := fmt.Errorf("no projects to watch; pass --project or bind this agent to a project")
		_ = emitter.emitStderr(eventsError("", true, err.Error()))
		return err
	}

	ctx, stopSignals := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	seen := &eventsDeduper{seen: map[string]struct{}{}}
	if !opts.noCatchup {
		runEventsCatchup(ctx, client, identity, projects, opts.filters, emitter, seen)
	}

	readyCh := make(chan eventsReadyState, len(projects))
	connectedCh := make(chan string, len(projects)*4)
	doneCh := make(chan struct{})
	var wg sync.WaitGroup
	for _, projectID := range projects {
		projectID := projectID
		wg.Add(1)
		go func() {
			defer wg.Done()
			watchEventsProject(ctx, client, token, identity, projectID, opts, emitter, seen, func(connected bool) {
				readyCh <- eventsReadyState{projectID: projectID, connected: connected}
			}, func() {
				select {
				case connectedCh <- projectID:
				default:
				}
			})
		}()
	}
	go func() {
		wg.Wait()
		close(doneCh)
	}()

	connectedAtReady, err := waitEventsReady(ctx, readyCh, len(projects), eventsStartupTimeout)
	if err != nil {
		_ = emitter.emitStderr(eventsError("", true, err.Error()))
		cancel()
		<-doneCh
		return err
	}
	if err := emitter.emitStdout(eventsOutput{Kind: eventKindReady, TS: eventsNow(), Projects: projects}); err != nil {
		cancel()
		<-doneCh
		return err
	}
	if len(connectedAtReady) == 0 {
		if err := waitAnyEventsProjectConnected(ctx, connectedCh, doneCh, eventsStartupTimeout); err != nil {
			_ = emitter.emitStderr(eventsError("", true, err.Error()))
			cancel()
			<-doneCh
			return err
		}
	}

	select {
	case <-ctx.Done():
		_ = emitter.emitStdout(eventsOutput{Kind: eventKindShutdown, TS: eventsNow()})
		return nil
	case <-doneCh:
		err := fmt.Errorf("all project event streams closed")
		_ = emitter.emitStderr(eventsError("", true, err.Error()))
		return err
	}
}

func loadEventsIdentity(env *CommandEnv, client *Client) (eventsIdentity, error) {
	ctx, cancel := env.context()
	defer cancel()
	res, err := client.WhoAmI(ctx)
	if err != nil {
		return eventsIdentity{}, err
	}
	return eventsIdentityFromProto(res.GetIdentity()), nil
}

func eventsIdentityFromProto(identity *aitaskv1.AgentIdentity) eventsIdentity {
	if identity == nil {
		return eventsIdentity{}
	}
	return eventsIdentity{
		AgentID:         strings.TrimSpace(identity.GetAgentId()),
		AgentType:       strings.TrimSpace(identity.GetAgentType()),
		AllowedProjects: append([]string{}, identity.GetAllowedProjects()...),
	}
}

func validateEventsOptions(opts eventsOptions) error {
	if strings.TrimSpace(opts.outputFile) == "" && !opts.stdout {
		return fmt.Errorf("at least one NDJSON sink must be enabled")
	}
	if opts.rotateSize <= 0 {
		return fmt.Errorf("--rotate-size must be positive")
	}
	if opts.rotateBackups < 0 {
		return fmt.Errorf("--rotate-backups cannot be negative")
	}
	switch strings.TrimSpace(opts.notify) {
	case "none", "", "osascript", "terminal-notifier", "auto":
	default:
		return fmt.Errorf("--notify must be one of none, osascript, terminal-notifier, auto")
	}
	if opts.pingInterval <= 0 {
		return fmt.Errorf("--ping-interval must be positive")
	}
	if opts.reconnectBase <= 0 {
		return fmt.Errorf("--reconnect-base must be positive")
	}
	if opts.reconnectMax <= 0 {
		return fmt.Errorf("--reconnect-max must be positive")
	}
	if opts.reconnectMax < opts.reconnectBase {
		return fmt.Errorf("--reconnect-max must be greater than or equal to --reconnect-base")
	}
	return nil
}

func parseEventsFilters(raw []string) (eventsFilterSet, error) {
	if len(raw) == 0 {
		raw = []string{eventKindMention, eventKindTaskDelegated}
	}
	out := eventsFilterSet{}
	for _, item := range raw {
		for _, part := range strings.Split(item, ",") {
			kind := strings.TrimSpace(part)
			if kind == "" {
				continue
			}
			if _, ok := validEventsFilterKind[kind]; !ok {
				return nil, fmt.Errorf("unsupported events filter %q", kind)
			}
			if kind == eventKindAll {
				return eventsFilterSet{eventKindAll: true}, nil
			}
			out[kind] = true
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("at least one --filter value is required")
	}
	return out, nil
}

func parseEventsNotifyKinds(raw []string) ([]string, error) {
	if len(raw) == 0 {
		return []string{eventKindMention, eventKindTaskDelegated}, nil
	}
	var out []string
	seen := map[string]struct{}{}
	for _, item := range raw {
		for _, part := range strings.Split(item, ",") {
			kind := strings.TrimSpace(part)
			if kind == "" {
				continue
			}
			if _, ok := validEventsFilterKind[kind]; !ok || kind == eventKindAll {
				return nil, fmt.Errorf("unsupported notify kind %q", kind)
			}
			if _, ok := seen[kind]; ok {
				continue
			}
			seen[kind] = struct{}{}
			out = append(out, kind)
		}
	}
	return out, nil
}

func resolveEventsProjects(explicit []string, allowed []string) []string {
	if len(explicit) > 0 {
		return dedupeStrings(explicit)
	}
	return dedupeStrings(allowed)
}

func runEventsCatchup(ctx context.Context, client *Client, identity eventsIdentity, projects []string, filters eventsFilterSet, emitter *eventsEmitter, seen *eventsDeduper) {
	var wg sync.WaitGroup
	for _, projectID := range projects {
		projectID := projectID
		wg.Add(1)
		go func() {
			defer wg.Done()
			if filters.allows(eventKindMention) {
				catchupMentions(ctx, client, identity, projectID, emitter, seen)
			}
			if filters.allows(eventKindTaskDelegated) {
				catchupDelegatedTasks(ctx, client, identity, projectID, emitter, seen)
			}
		}()
	}
	wg.Wait()
}

func catchupMentions(ctx context.Context, client *Client, identity eventsIdentity, projectID string, emitter *eventsEmitter, seen *eventsDeduper) {
	payload, err := client.GetREST(ctx, "/api/projects/"+projectID+"/room/mentions", map[string]string{"onlyUnhandled": "true"})
	if err != nil {
		_ = emitter.emitStdout(eventsError(projectID, false, "catch-up mentions failed: "+err.Error()))
		return
	}
	for _, item := range asSlice(payload["items"]) {
		mention := asMap(item)
		if mapBool(mention, "handled") {
			continue
		}
		event := catchupMentionEvent(projectID, mention, identity)
		if seen.mark(event.EventID) {
			_ = emitter.emitStdout(event)
		}
	}
}

func catchupDelegatedTasks(ctx context.Context, client *Client, identity eventsIdentity, projectID string, emitter *eventsEmitter, seen *eventsDeduper) {
	payload, err := client.GetREST(ctx, "/api/projects/"+projectID+"/tasks", map[string]string{"status": "delegated", "assigneeAgentId": identity.AgentID})
	if err != nil {
		_ = emitter.emitStdout(eventsError(projectID, false, "catch-up delegated tasks failed: "+err.Error()))
		return
	}
	for _, item := range asSlice(payload["items"]) {
		task := asMap(item)
		if mapString(task, "status") != "delegated" || mapString(task, "assigneeAgentId") != identity.AgentID {
			continue
		}
		event := catchupTaskDelegatedEvent(projectID, task)
		if seen.mark(event.EventID) {
			_ = emitter.emitStdout(event)
		}
	}
}

func watchEventsProject(
	ctx context.Context,
	client *Client,
	token string,
	identity eventsIdentity,
	projectID string,
	opts eventsOptions,
	emitter *eventsEmitter,
	seen *eventsDeduper,
	markReady func(bool),
	markConnected func(),
) {
	attempt := 0
	readyMarked := false
	markReadyOnce := func(connected bool) {
		if readyMarked {
			return
		}
		readyMarked = true
		markReady(connected)
	}

	for ctx.Err() == nil {
		wsURL, err := client.WebSocketURL(projectID)
		if err != nil {
			_ = emitter.emitStdout(eventsError(projectID, true, err.Error()))
			markReadyOnce(false)
			return
		}
		header := http.Header{}
		if strings.TrimSpace(token) != "" {
			header.Set("Authorization", "Bearer "+token)
		}

		conn, resp, err := websocket.DefaultDialer.DialContext(ctx, wsURL, header)
		if err != nil {
			markReadyOnce(false)
			if isPermanentWebSocketFailure(resp) {
				_ = emitter.emitStdout(eventsError(projectID, true, err.Error()))
				return
			}
			attempt++
			_ = emitter.emitStdout(eventsReconnect(projectID, attempt, err.Error()))
			if !sleepEventsBackoff(ctx, attempt, opts.reconnectBase, opts.reconnectMax) {
				return
			}
			continue
		}

		err = readEventsConnection(ctx, conn, identity, projectID, opts, emitter, seen, func() {
			attempt = 0
			markReadyOnce(true)
			markConnected()
		})
		_ = conn.Close()
		if ctx.Err() != nil {
			return
		}
		attempt++
		_ = emitter.emitStdout(eventsReconnect(projectID, attempt, err.Error()))
		if !sleepEventsBackoff(ctx, attempt, opts.reconnectBase, opts.reconnectMax) {
			return
		}
	}
}

func readEventsConnection(
	ctx context.Context,
	conn *websocket.Conn,
	identity eventsIdentity,
	projectID string,
	opts eventsOptions,
	emitter *eventsEmitter,
	seen *eventsDeduper,
	markConnected func(),
) error {
	pingCtx, stopPing := context.WithCancel(ctx)
	defer stopPing()
	go eventsPingLoop(pingCtx, conn, opts.pingInterval)
	closeDone := make(chan struct{})
	defer close(closeDone)
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-closeDone:
		}
	}()

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		var envelope roomEventEnvelope
		if err := json.Unmarshal(msg, &envelope); err != nil {
			_ = emitter.emitStdout(eventsError(projectID, false, "decode envelope failed: "+err.Error()))
			continue
		}
		if envelope.ProjectID == "" {
			envelope.ProjectID = projectID
		}
		if envelope.EventType == "room.connected" {
			markConnected()
		}
		if !seen.mark(envelope.EventID) {
			continue
		}
		event, ok := eventFromEnvelope(envelope, identity, opts.filters, opts.includeSelf)
		if !ok {
			continue
		}
		_ = emitter.emitStdout(event)
	}
}

func eventsPingLoop(ctx context.Context, conn *websocket.Conn, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = conn.WriteJSON(map[string]any{"type": "ping", "sentAt": time.Now().UTC().Format(time.RFC3339)})
		}
	}
}

func waitEventsReady(ctx context.Context, readyCh <-chan eventsReadyState, want int, timeout time.Duration) (map[string]struct{}, error) {
	if want <= 0 {
		return map[string]struct{}{}, nil
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	seen := map[string]struct{}{}
	connected := map[string]struct{}{}
	for len(seen) < want {
		select {
		case <-ctx.Done():
			return connected, ctx.Err()
		case <-timer.C:
			return connected, fmt.Errorf("event streams did not become ready within %s", timeout)
		case state := <-readyCh:
			projectID := strings.TrimSpace(state.projectID)
			if projectID != "" {
				seen[projectID] = struct{}{}
				if state.connected {
					connected[projectID] = struct{}{}
				}
			}
		}
	}
	return connected, nil
}

func waitAnyEventsProjectConnected(ctx context.Context, connectedCh <-chan string, doneCh <-chan struct{}, timeout time.Duration) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-doneCh:
			return fmt.Errorf("all project event streams closed before connecting")
		case <-timer.C:
			return fmt.Errorf("no project event stream connected within %s", timeout)
		case projectID := <-connectedCh:
			if strings.TrimSpace(projectID) != "" {
				return nil
			}
		}
	}
}

func eventFromEnvelope(envelope roomEventEnvelope, identity eventsIdentity, filters eventsFilterSet, includeSelf bool) (eventsOutput, bool) {
	projectID := nonEmptyString(envelope.ProjectID, envelope.ProjectID)
	if envelope.CreatedAt.IsZero() {
		envelope.CreatedAt = time.Now().UTC()
	}
	if filters[eventKindAll] {
		return eventsOutput{
			Kind:    eventKindEvent,
			TS:      eventsTS(envelope.CreatedAt),
			Project: projectID,
			EventID: envelope.EventID,
			From:    &envelope.Sender,
			Details: map[string]any{
				"eventType": envelope.EventType,
				"roomId":    envelope.RoomID,
				"payload":   envelope.Payload,
			},
		}, true
	}

	switch envelope.EventType {
	case "room.message":
		return eventFromRoomMessage(envelope, identity, filters, includeSelf)
	case "task.updated":
		return eventFromTaskUpdated(envelope, identity, filters)
	default:
		return eventsOutput{}, false
	}
}

func eventFromRoomMessage(envelope roomEventEnvelope, identity eventsIdentity, filters eventsFilterSet, includeSelf bool) (eventsOutput, bool) {
	payload := envelope.Payload
	content := mapString(payload, "content")
	messageID := mapString(payload, "messageId")
	messageType := mapString(payload, "messageType")
	if filters.allows(eventKindMention) && (includeSelf || !senderIsSelf(envelope.Sender, identity)) {
		mentions := matchingMentionTokens(parseMentionTokens(content), identity)
		if len(mentions) > 0 {
			return eventsOutput{
				Kind:      eventKindMention,
				TS:        eventsTS(envelope.CreatedAt),
				Project:   envelope.ProjectID,
				EventID:   envelope.EventID,
				MessageID: messageID,
				From:      &envelope.Sender,
				Content:   content,
				Mentions:  mentions,
			}, true
		}
	}
	if filters.allows(eventKindSystemEvent) && messageType == "system_event" {
		return eventsOutput{
			Kind:      eventKindSystemEvent,
			TS:        eventsTS(envelope.CreatedAt),
			Project:   envelope.ProjectID,
			EventID:   envelope.EventID,
			MessageID: messageID,
			From:      &envelope.Sender,
			Content:   content,
			Details:   asMap(payload["payload"]),
		}, true
	}
	if filters.allows(eventKindRoomMessage) {
		return eventsOutput{
			Kind:      eventKindRoomMessage,
			TS:        eventsTS(envelope.CreatedAt),
			Project:   envelope.ProjectID,
			EventID:   envelope.EventID,
			MessageID: messageID,
			From:      &envelope.Sender,
			Content:   content,
			Details:   asMap(payload["payload"]),
		}, true
	}
	return eventsOutput{}, false
}

func eventFromTaskUpdated(envelope roomEventEnvelope, identity eventsIdentity, filters eventsFilterSet) (eventsOutput, bool) {
	payload := envelope.Payload
	eventType := mapString(payload, "eventType")
	assigneeID := eventAssigneeAgentID(payload)
	if eventType == "task.delegated" {
		if filters.allows(eventKindTaskDelegated) && assigneeID == identity.AgentID {
			return taskEventOutput(eventKindTaskDelegated, envelope, assigneeID), true
		}
		return eventsOutput{}, false
	}
	if filters.allows(eventKindTaskUpdated) && assigneeID == identity.AgentID {
		return taskEventOutput(eventKindTaskUpdated, envelope, assigneeID), true
	}
	return eventsOutput{}, false
}

func taskEventOutput(kind string, envelope roomEventEnvelope, assigneeID string) eventsOutput {
	payload := envelope.Payload
	return eventsOutput{
		Kind:            kind,
		TS:              eventsTS(envelope.CreatedAt),
		Project:         envelope.ProjectID,
		EventID:         envelope.EventID,
		TaskID:          mapString(payload, "taskId"),
		AssigneeAgentID: assigneeID,
		FromStatus:      mapString(payload, "fromStatus"),
		ToStatus:        mapString(payload, "toStatus"),
		Details:         asMap(payload["details"]),
	}
}

func catchupMentionEvent(projectID string, item map[string]any, identity eventsIdentity) eventsOutput {
	mentionID := mapString(item, "mentionId")
	messageID := mapString(item, "messageId")
	mentions := catchupMentionTargets(item, identity)
	return eventsOutput{
		Kind:      eventKindMention,
		TS:        eventTimeFromMap(item, "createdAt"),
		Project:   projectID,
		EventID:   "catchup:mention:" + nonEmptyString(mentionID, messageID),
		MessageID: messageID,
		Mentions:  mentions,
		Catchup:   boolPtr(true),
	}
}

func catchupTaskDelegatedEvent(projectID string, task map[string]any) eventsOutput {
	taskID := mapString(task, "taskId")
	return eventsOutput{
		Kind:            eventKindTaskDelegated,
		TS:              eventTimeFromMap(task, "updatedAt"),
		Project:         projectID,
		EventID:         "catchup:task_delegated:" + taskID,
		TaskID:          taskID,
		AssigneeAgentID: mapString(task, "assigneeAgentId"),
		ToStatus:        "delegated",
		Details:         task,
		Catchup:         boolPtr(true),
	}
}

func catchupMentionTargets(item map[string]any, identity eventsIdentity) []string {
	candidates := []string{
		mapString(item, "mentionedAgentId"),
		mapString(item, "mentionedAgentType"),
		mapString(item, "operatorLabel"),
	}
	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate != "" {
			out = append(out, candidate)
		}
	}
	if len(out) == 0 {
		out = matchingMentionTokens([]string{identity.AgentID, identity.AgentType, identity.OperatorLabel}, identity)
	}
	return dedupeStrings(out)
}

func parseMentionTokens(content string) []string {
	matches := eventsMentionPattern.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return []string{}
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		token := strings.TrimSpace(match[1])
		if token == "" {
			continue
		}
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		out = append(out, token)
	}
	return out
}

func matchingMentionTokens(tokens []string, identity eventsIdentity) []string {
	targets := map[string]struct{}{}
	for _, target := range []string{identity.AgentID, identity.AgentType, identity.OperatorLabel} {
		target = strings.TrimSpace(target)
		if target != "" {
			targets[target] = struct{}{}
		}
	}
	out := make([]string, 0, len(tokens))
	for _, token := range tokens {
		token = strings.TrimSpace(token)
		if _, ok := targets[token]; ok {
			out = append(out, token)
		}
	}
	return dedupeStrings(out)
}

func senderIsSelf(sender eventsSender, identity eventsIdentity) bool {
	if sender.AgentID != nil && strings.TrimSpace(*sender.AgentID) != "" && strings.TrimSpace(*sender.AgentID) == identity.AgentID {
		return true
	}
	return sender.AgentID == nil && sender.AgentType != nil && strings.TrimSpace(*sender.AgentType) == identity.AgentType
}

func eventAssigneeAgentID(payload map[string]any) string {
	for _, source := range []map[string]any{asMap(payload["details"]), payload, asMap(payload["task"])} {
		if value := mapString(source, "assigneeAgentId"); value != "" {
			return value
		}
		if value := mapString(source, "assignee_agent_id"); value != "" {
			return value
		}
	}
	return ""
}

func serializeEvent(event eventsOutput) ([]byte, error) {
	return json.Marshal(event)
}

func (e *eventsEmitter) emitStdout(event eventsOutput) error {
	if err := e.emitAll(e.stream, event); err != nil {
		return err
	}
	e.notify(event)
	return nil
}

func (e *eventsEmitter) emitStderr(event eventsOutput) error {
	return e.emit(e.stderr, event)
}

func (e *eventsEmitter) emitAll(writers []io.Writer, event eventsOutput) error {
	payload, err := serializeEvent(event)
	if err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	var errs []error
	for _, w := range writers {
		if w == nil {
			continue
		}
		if _, err := fmt.Fprintln(w, string(payload)); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (e *eventsEmitter) emit(w io.Writer, event eventsOutput) error {
	if w == nil {
		return nil
	}
	payload, err := serializeEvent(event)
	if err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	_, err = fmt.Fprintln(w, string(payload))
	return err
}

func (e *eventsEmitter) notify(event eventsOutput) {
	if e == nil || e.notifier == nil {
		return
	}
	_ = e.notifier.Notify(event)
}

func (e *eventsEmitter) Close() error {
	if e == nil || e.closer == nil {
		return nil
	}
	return e.closer.Close()
}

func (d *eventsDeduper) mark(eventID string) bool {
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return true
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.seen[eventID]; ok {
		return false
	}
	d.seen[eventID] = struct{}{}
	return true
}

func eventsReconnect(projectID string, attempt int, reason string) eventsOutput {
	return eventsOutput{
		Kind:    eventKindReconnect,
		TS:      eventsNow(),
		Project: projectID,
		Attempt: attempt,
		Reason:  strings.TrimSpace(reason),
	}
}

func eventsError(projectID string, fatal bool, reason string) eventsOutput {
	return eventsOutput{
		Kind:    eventKindError,
		TS:      eventsNow(),
		Project: projectID,
		Fatal:   boolPtr(fatal),
		Reason:  strings.TrimSpace(reason),
	}
}

func sleepEventsBackoff(ctx context.Context, attempt int, base time.Duration, max time.Duration) bool {
	delay := eventsReconnectDelay(attempt, base, max, func() float64 {
		return rand.Float64()*2 - 1
	})
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func eventsReconnectDelay(attempt int, base time.Duration, max time.Duration, jitter func() float64) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := base
	for i := 1; i < attempt && delay < max; i++ {
		delay *= 2
		if delay > max {
			delay = max
			break
		}
	}
	if jitter != nil {
		factor := 1 + clampFloat(jitter(), -1, 1)*0.2
		delay = time.Duration(float64(delay) * factor)
		if delay > max {
			delay = max
		}
	}
	if delay < 0 {
		return 0
	}
	return delay
}

func isPermanentWebSocketFailure(resp *http.Response) bool {
	if resp == nil {
		return false
	}
	return resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != http.StatusRequestTimeout && resp.StatusCode != http.StatusTooManyRequests
}

func eventTimeFromMap(item map[string]any, key string) string {
	value := mapString(item, key)
	if value == "" {
		return eventsNow()
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return eventsNow()
	}
	return eventsTS(parsed)
}

func eventsNow() string {
	return eventsTS(time.Now().UTC())
}

func eventsTS(value time.Time) string {
	if value.IsZero() {
		value = time.Now().UTC()
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func dedupeStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func nonEmptyString(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func boolPtr(value bool) *bool {
	return &value
}

func stringPtr(value string) *string {
	return &value
}

func clampFloat(value float64, min float64, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func sortedEventFilterKinds(filters eventsFilterSet) []string {
	out := make([]string, 0, len(filters))
	for kind := range filters {
		out = append(out, kind)
	}
	sort.Strings(out)
	return out
}

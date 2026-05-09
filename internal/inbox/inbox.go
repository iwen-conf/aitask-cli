package inbox

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

var ErrNotApplicable = errors.New("inbox status update not applicable")

type ListOpts struct {
	Limit       int
	Status      string
	AllStatuses bool
	Project     string
}

type EventRow struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Scope     string `json:"scope,omitempty"`
	Project   string `json:"project,omitempty"`
	ThreadID  string `json:"threadId,omitempty"`
	FromAgent string `json:"fromAgent,omitempty"`
	ToAgent   string `json:"toAgent,omitempty"`
	Body      string `json:"body,omitempty"`
	RawJSON   string `json:"rawJson,omitempty"`
	CreatedAt string `json:"createdAt"`
	IndexedAt string `json:"indexedAt,omitempty"`
}

type InboxRow struct {
	EventRow
	InboxID    int64  `json:"inboxId,omitempty"`
	Agent      string `json:"agent,omitempty"`
	Status     string `json:"status,omitempty"`
	SeenAt     string `json:"seenAt,omitempty"`
	AckedAt    string `json:"ackedAt,omitempty"`
	HandledAt  string `json:"handledAt,omitempty"`
	FailedAt   string `json:"failedAt,omitempty"`
	RetryCount int    `json:"retryCount,omitempty"`
	LastError  string `json:"lastError,omitempty"`
	Visibility string `json:"visibility,omitempty"`
	Wake       bool   `json:"wake,omitempty"`
}

type StatusResult struct {
	EventID      string `json:"eventId"`
	Agent        string `json:"agent"`
	Status       string `json:"status"`
	RowsAffected int64  `json:"rowsAffected"`
}

type ndjsonEvent struct {
	Kind            string          `json:"kind"`
	TS              string          `json:"ts"`
	Project         string          `json:"project"`
	EventID         string          `json:"eventId"`
	MessageID       string          `json:"messageId"`
	TaskID          string          `json:"taskId"`
	AssigneeAgentID string          `json:"assigneeAgentId"`
	From            json.RawMessage `json:"from"`
	Content         string          `json:"content"`
	Mentions        []string        `json:"mentions"`
	Details         map[string]any  `json:"details"`
}

type sender struct {
	Type          string  `json:"type"`
	AgentType     *string `json:"agentType"`
	AgentID       *string `json:"agentId"`
	OperatorLabel *string `json:"operatorLabel"`
}

func Ingest(ctx context.Context, db *sql.DB, ndjsonPath string) error {
	file, err := os.Open(ndjsonPath)
	if err != nil {
		return err
	}
	defer file.Close()

	startOffset, err := cursorOffset(ctx, db, "worker:indexer", ndjsonPath)
	if err != nil {
		return err
	}
	if startOffset > 0 {
		info, err := file.Stat()
		if err != nil {
			return err
		}
		if info.Size() >= startOffset {
			if _, err := file.Seek(startOffset, 0); err != nil {
				return err
			}
		} else {
			startOffset = 0
		}
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	offset := startOffset
	var lastEventID string
	for scanner.Scan() {
		line := scanner.Bytes()
		offset += int64(len(line)) + 1
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		var ev ndjsonEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}
		if strings.TrimSpace(ev.EventID) == "" || strings.TrimSpace(ev.Kind) == "" {
			continue
		}
		raw := string(append([]byte(nil), line...))
		if err := ingestEvent(ctx, tx, ev, raw); err != nil {
			return err
		}
		lastEventID = ev.EventID
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := tx.ExecContext(ctx, `INSERT INTO cursors(consumer, source, offset, event_id, updated_at)
VALUES ('worker:indexer', ?, ?, ?, ?)
ON CONFLICT(consumer) DO UPDATE SET source=excluded.source, offset=excluded.offset, event_id=excluded.event_id, updated_at=excluded.updated_at`,
		ndjsonPath, offset, nullString(lastEventID), now); err != nil {
		return err
	}
	return tx.Commit()
}

func cursorOffset(ctx context.Context, db *sql.DB, consumer, source string) (int64, error) {
	var offset int64
	var cursorSource string
	err := db.QueryRowContext(ctx, `SELECT source, offset FROM cursors WHERE consumer=?`, consumer).Scan(&cursorSource, &offset)
	if err == nil {
		if cursorSource != source {
			return 0, nil
		}
		return offset, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return 0, err
}

func ListAgentInbox(ctx context.Context, db *sql.DB, agent string, opts ListOpts) ([]InboxRow, error) {
	agent = strings.TrimSpace(agent)
	if agent == "" {
		return nil, fmt.Errorf("agent cannot be empty")
	}
	limit := normalizeLimit(opts.Limit)
	statusClause, statusArgs := statusFilterSQL(opts)
	args := []any{agent, agent}
	args = append(args, statusArgs...)
	args = append(args, limit)
	rows, err := db.QueryContext(ctx, `SELECT
  ai.id, ai.agent, ai.status, ai.seen_at, ai.acked_at, ai.handled_at, ai.failed_at, ai.retry_count, ai.last_error,
  e.id, e.kind, e.scope, e.project, e.thread_id, e.from_agent, e.to_agent, e.body, e.raw_json, e.created_at, e.indexed_at
FROM agent_inbox ai
JOIN events e ON e.id = ai.event_id
WHERE ai.agent = ? AND COALESCE(e.from_agent, '') <> ?`+statusClause+`
ORDER BY e.created_at ASC
LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []InboxRow{}
	for rows.Next() {
		row, err := scanInboxRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func ListGlobalFeed(ctx context.Context, db *sql.DB, opts ListOpts) ([]InboxRow, error) {
	limit := normalizeLimit(opts.Limit)
	project := strings.TrimSpace(opts.Project)
	args := []any{}
	where := ""
	if project != "" {
		where = "WHERE gf.project = ? OR gf.visibility = 'broadcast'"
		args = append(args, project)
	}
	args = append(args, limit)
	rows, err := db.QueryContext(ctx, `SELECT
  gf.id, '', '', '', '', '', '', 0, '',
  e.id, e.kind, e.scope, e.project, e.thread_id, e.from_agent, e.to_agent, e.body, e.raw_json, e.created_at, e.indexed_at,
  gf.visibility, gf.wake
FROM global_feed gf
JOIN events e ON e.id = gf.event_id
`+where+`
ORDER BY gf.created_at DESC
LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []InboxRow{}
	for rows.Next() {
		row, err := scanGlobalRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func ListLatest(ctx context.Context, db *sql.DB, limit int) ([]EventRow, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, kind, scope, project, thread_id, from_agent, to_agent, body, raw_json, created_at, indexed_at
FROM events
ORDER BY created_at DESC
LIMIT ?`, normalizeLimit(limit))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEventRows(rows)
}

func ListThread(ctx context.Context, db *sql.DB, threadID string) ([]EventRow, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, kind, scope, project, thread_id, from_agent, to_agent, body, raw_json, created_at, indexed_at
FROM events
WHERE thread_id = ?
ORDER BY created_at ASC`, strings.TrimSpace(threadID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEventRows(rows)
}

func Ack(ctx context.Context, db *sql.DB, eventID, agent string) (StatusResult, error) {
	return updateStatus(ctx, db, eventID, agent, "acked", "acked_at", "", []string{"unread", "seen"})
}

func Done(ctx context.Context, db *sql.DB, eventID, agent string) (StatusResult, error) {
	return updateStatus(ctx, db, eventID, agent, "handled", "handled_at", "", []string{"acked"})
}

func Fail(ctx context.Context, db *sql.DB, eventID, agent, message string) (StatusResult, error) {
	return updateStatus(ctx, db, eventID, agent, "failed", "failed_at", message, []string{"acked"})
}

func Skip(ctx context.Context, db *sql.DB, eventID, agent, reason string) (StatusResult, error) {
	return updateStatus(ctx, db, eventID, agent, "skipped", "handled_at", reason, []string{"acked", "failed"})
}

func ingestEvent(ctx context.Context, tx *sql.Tx, ev ndjsonEvent, raw string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	created := nonEmpty(ev.TS, now)
	body := eventBody(ev)
	fromAgent := fromAgent(ev)
	toAgents := eventTargets(ev)
	toAgent := ""
	if len(toAgents) == 1 {
		toAgent = toAgents[0]
	}
	threadID := eventThreadID(ev)
	if _, err := tx.ExecContext(ctx, `INSERT INTO events(id, kind, scope, project, thread_id, from_agent, to_agent, body, raw_json, created_at, indexed_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  kind=excluded.kind, scope=excluded.scope, project=excluded.project, thread_id=excluded.thread_id,
  from_agent=excluded.from_agent, to_agent=excluded.to_agent, body=excluded.body, raw_json=excluded.raw_json,
  created_at=excluded.created_at, indexed_at=excluded.indexed_at`,
		ev.EventID, ev.Kind, eventScope(ev), nullString(ev.Project), nullString(threadID), nullString(fromAgent), nullString(toAgent), nullString(body), raw, created, now); err != nil {
		return err
	}
	for _, agent := range toAgents {
		if _, err := tx.ExecContext(ctx, `INSERT INTO agent_inbox(event_id, agent, status)
VALUES (?, ?, 'unread')
ON CONFLICT(event_id, agent) DO NOTHING`, ev.EventID, agent); err != nil {
			return err
		}
	}
	if isGlobal(ev) {
		visibility := "project"
		if strings.EqualFold(ev.Kind, "broadcast") {
			visibility = "broadcast"
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO global_feed(event_id, project, visibility, wake, created_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(event_id) DO UPDATE SET project=excluded.project, visibility=excluded.visibility, wake=excluded.wake, created_at=excluded.created_at`,
			ev.EventID, nullString(ev.Project), visibility, eventWake(ev), created); err != nil {
			return err
		}
	}
	return nil
}

func updateStatus(ctx context.Context, db *sql.DB, eventID, agent, next, stampColumn, extra string, allowed []string) (StatusResult, error) {
	eventID = strings.TrimSpace(eventID)
	agent = strings.TrimSpace(agent)
	if eventID == "" || agent == "" {
		return StatusResult{}, fmt.Errorf("event_id and agent are required")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	placeholders := strings.TrimRight(strings.Repeat("?,", len(allowed)), ",")
	args := []any{next, now}
	setExtra := ""
	if next == "failed" {
		setExtra = ", retry_count = retry_count + 1, last_error = ?"
		args = append(args, extra)
	} else if next == "skipped" {
		setExtra = ", last_error = ?"
		args = append(args, extra)
	}
	args = append(args, eventID, agent)
	for _, status := range allowed {
		args = append(args, status)
	}
	res, err := db.ExecContext(ctx, fmt.Sprintf(`UPDATE agent_inbox
SET status = ?, %s = ?%s
WHERE event_id = ? AND agent = ? AND status IN (%s)`, stampColumn, setExtra, placeholders), args...)
	if err != nil {
		return StatusResult{}, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return StatusResult{}, err
	}
	if affected == 0 {
		return StatusResult{}, ErrNotApplicable
	}
	return StatusResult{EventID: eventID, Agent: agent, Status: next, RowsAffected: affected}, nil
}

func statusFilterSQL(opts ListOpts) (string, []any) {
	if opts.AllStatuses || strings.TrimSpace(opts.Status) == "all" {
		return "", nil
	}
	status := strings.TrimSpace(opts.Status)
	if status == "" {
		return " AND ai.status IN ('unread','seen','acked')", nil
	}
	parts := strings.Split(status, ",")
	placeholders := make([]string, 0, len(parts))
	args := make([]any, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		placeholders = append(placeholders, "?")
		args = append(args, part)
	}
	if len(args) == 0 {
		return " AND ai.status IN ('unread','seen','acked')", nil
	}
	return " AND ai.status IN (" + strings.Join(placeholders, ",") + ")", args
}

func scanInboxRow(rows *sql.Rows) (InboxRow, error) {
	var row InboxRow
	var seenAt, ackedAt, handledAt, failedAt, lastError sql.NullString
	var scope, project, threadID, fromAgent, toAgent, body sql.NullString
	err := rows.Scan(&row.InboxID, &row.Agent, &row.Status, &seenAt, &ackedAt, &handledAt, &failedAt, &row.RetryCount, &lastError,
		&row.ID, &row.Kind, &scope, &project, &threadID, &fromAgent, &toAgent, &body, &row.RawJSON, &row.CreatedAt, &row.IndexedAt)
	if err != nil {
		return InboxRow{}, err
	}
	row.SeenAt = seenAt.String
	row.AckedAt = ackedAt.String
	row.HandledAt = handledAt.String
	row.FailedAt = failedAt.String
	row.LastError = lastError.String
	row.Scope = scope.String
	row.Project = project.String
	row.ThreadID = threadID.String
	row.FromAgent = fromAgent.String
	row.ToAgent = toAgent.String
	row.Body = body.String
	return row, nil
}

func scanGlobalRow(rows *sql.Rows) (InboxRow, error) {
	var row InboxRow
	var scope, project, threadID, fromAgent, toAgent, body sql.NullString
	var wake int
	err := rows.Scan(&row.InboxID, &row.Agent, &row.Status, &row.SeenAt, &row.AckedAt, &row.HandledAt, &row.FailedAt, &row.RetryCount, &row.LastError,
		&row.ID, &row.Kind, &scope, &project, &threadID, &fromAgent, &toAgent, &body, &row.RawJSON, &row.CreatedAt, &row.IndexedAt,
		&row.Visibility, &wake)
	if err != nil {
		return InboxRow{}, err
	}
	row.Scope = scope.String
	row.Project = project.String
	row.ThreadID = threadID.String
	row.FromAgent = fromAgent.String
	row.ToAgent = toAgent.String
	row.Body = body.String
	row.Wake = wake != 0
	return row, nil
}

func scanEventRows(rows *sql.Rows) ([]EventRow, error) {
	out := []EventRow{}
	for rows.Next() {
		var row EventRow
		var scope, project, threadID, fromAgent, toAgent, body sql.NullString
		if err := rows.Scan(&row.ID, &row.Kind, &scope, &project, &threadID, &fromAgent, &toAgent, &body, &row.RawJSON, &row.CreatedAt, &row.IndexedAt); err != nil {
			return nil, err
		}
		row.Scope = scope.String
		row.Project = project.String
		row.ThreadID = threadID.String
		row.FromAgent = fromAgent.String
		row.ToAgent = toAgent.String
		row.Body = body.String
		out = append(out, row)
	}
	return out, rows.Err()
}

func eventTargets(ev ndjsonEvent) []string {
	seen := map[string]struct{}{}
	out := []string{}
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	for _, mention := range ev.Mentions {
		add(mention)
	}
	add(ev.AssigneeAgentID)
	return out
}

func fromAgent(ev ndjsonEvent) string {
	if len(ev.From) == 0 {
		return ""
	}
	var from sender
	if err := json.Unmarshal(ev.From, &from); err != nil {
		return ""
	}
	for _, value := range []*string{from.AgentType, from.OperatorLabel, from.AgentID} {
		if value != nil && strings.TrimSpace(*value) != "" {
			return strings.TrimSpace(*value)
		}
	}
	return strings.TrimSpace(from.Type)
}

func eventBody(ev ndjsonEvent) string {
	if strings.TrimSpace(ev.Content) != "" {
		return ev.Content
	}
	if strings.TrimSpace(ev.TaskID) != "" {
		return ev.TaskID
	}
	return ""
}

func eventThreadID(ev ndjsonEvent) string {
	if value, ok := ev.Details["thread_id"].(string); ok {
		return value
	}
	if value, ok := ev.Details["threadId"].(string); ok {
		return value
	}
	return nonEmpty(ev.MessageID, ev.TaskID)
}

func eventScope(ev ndjsonEvent) string {
	if strings.TrimSpace(ev.Project) != "" {
		return "project"
	}
	return ""
}

func isGlobal(ev ndjsonEvent) bool {
	switch ev.Kind {
	case "broadcast", "room_message", "system_event", "ready", "error", "daemon":
		return true
	default:
		return len(eventTargets(ev)) == 0 && strings.TrimSpace(ev.Project) != ""
	}
}

func eventWake(ev ndjsonEvent) int {
	if value, ok := ev.Details["wake"].(bool); ok && value {
		return 1
	}
	return 0
}

func normalizeLimit(limit int) int {
	if limit <= 0 {
		return 20
	}
	if limit > 500 {
		return 500
	}
	return limit
}

func nonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func nullString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return strings.TrimSpace(value)
}

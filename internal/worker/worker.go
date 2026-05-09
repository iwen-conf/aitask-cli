package worker

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	localinbox "github.com/iwen-conf/aitask-cli/internal/inbox"
)

const (
	defaultBatchSize  = 50
	defaultInterval   = 10 * time.Second
	defaultMaxRetries = 5
	maxSummaryBytes   = 4 * 1024
)

var ErrAlreadyRunning = errors.New("worker already running")

type Options struct {
	StateDB    *sql.DB
	NDJSONPath string
	Sync       Syncer
	Interval   time.Duration
	BatchSize  int
	MaxRetries int
	Logger     *log.Logger
	AutoSync   bool
}

type Syncer interface {
	WriteMemory(context.Context, WriteMemoryRequest) (WriteMemoryResponse, error)
}

type WriteMemoryRequest struct {
	ProjectID      string
	Target         string
	Title          string
	Content        string
	RelatedEventID string
	RelatedTaskID  string
}

type WriteMemoryResponse struct {
	URI      string
	MemoryID string
}

type Stats struct {
	Ingested         int `json:"ingested"`
	RoutedAgent      int `json:"routedAgent"`
	RoutedGlobal     int `json:"routedGlobal"`
	SyncSucceeded    int `json:"syncSucceeded"`
	SyncFailed       int `json:"syncFailed"`
	SummariesUpdated int `json:"summariesUpdated"`
}

type BackfillOptions struct {
	StateDB *sql.DB
	Since   time.Time
	Limit   int
	DryRun  bool
	Logger  *log.Logger
}

type BackfillStats struct {
	Matched  int      `json:"matched"`
	Inserted int      `json:"inserted"`
	DryRun   bool     `json:"dryRun"`
	EventIDs []string `json:"eventIds"`
}

type eventRow struct {
	ID        string
	Kind      string
	Project   string
	ThreadID  string
	Body      string
	RawJSON   string
	CreatedAt string
}

func RunOnce(ctx context.Context, opts Options) (Stats, error) {
	if opts.StateDB == nil {
		return Stats{}, fmt.Errorf("state db is required")
	}
	if strings.TrimSpace(opts.NDJSONPath) == "" {
		return Stats{}, fmt.Errorf("ndjson path is required")
	}
	release, err := acquireLock(opts)
	if err != nil {
		return Stats{}, err
	}
	defer release()

	before, err := knownEventIDs(ctx, opts.StateDB)
	if err != nil {
		return Stats{}, err
	}
	if err := localinbox.Ingest(ctx, opts.StateDB, opts.NDJSONPath); err != nil {
		return Stats{}, err
	}
	events, err := newEvents(ctx, opts.StateDB, before)
	if err != nil {
		return Stats{}, err
	}
	stats := Stats{Ingested: len(events)}
	if len(events) > 0 {
		routedAgent, routedGlobal, err := routeCounts(ctx, opts.StateDB, events)
		if err != nil {
			return Stats{}, err
		}
		stats.RoutedAgent = routedAgent
		stats.RoutedGlobal = routedGlobal
		if err := enqueueMemorySync(ctx, opts.StateDB, events); err != nil {
			return Stats{}, err
		}
		updated, err := refreshThreadSummaries(ctx, opts.StateDB, events)
		if err != nil {
			return Stats{}, err
		}
		stats.SummariesUpdated = updated
	}
	if opts.Sync != nil {
		succeeded, failed, err := syncPending(ctx, opts)
		if err != nil {
			return Stats{}, err
		}
		stats.SyncSucceeded = succeeded
		stats.SyncFailed = failed
	}
	logStats(opts, stats)
	return stats, nil
}

func BackfillMemorySync(ctx context.Context, opts BackfillOptions) (BackfillStats, error) {
	if opts.StateDB == nil {
		return BackfillStats{}, fmt.Errorf("state db is required")
	}
	if opts.Since.IsZero() {
		return BackfillStats{}, fmt.Errorf("backfill since timestamp is required")
	}
	release, err := acquireLock(Options{StateDB: opts.StateDB, Logger: opts.Logger})
	if err != nil {
		return BackfillStats{}, err
	}
	defer release()

	query := `SELECT e.id, e.kind
FROM events e
LEFT JOIN memory_sync ms ON ms.event_id = e.id
WHERE e.created_at >= ? AND ms.event_id IS NULL
ORDER BY e.created_at ASC, e.id ASC`
	args := []any{opts.Since.UTC().Format(time.RFC3339)}
	if opts.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, opts.Limit)
	}
	rows, err := opts.StateDB.QueryContext(ctx, query, args...)
	if err != nil {
		return BackfillStats{}, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		var kind string
		if err := rows.Scan(&id, &kind); err != nil {
			return BackfillStats{}, err
		}
		if !syncableKind(kind) {
			continue
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return BackfillStats{}, err
	}
	stats := BackfillStats{Matched: len(ids), DryRun: opts.DryRun, EventIDs: ids}
	if opts.DryRun || len(ids) == 0 {
		return stats, nil
	}
	tx, err := opts.StateDB.BeginTx(ctx, nil)
	if err != nil {
		return BackfillStats{}, err
	}
	defer tx.Rollback()
	for _, id := range ids {
		res, err := tx.ExecContext(ctx, `INSERT INTO memory_sync(event_id, status)
VALUES (?, 'pending')
ON CONFLICT(event_id) DO NOTHING`, id)
		if err != nil {
			return BackfillStats{}, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			stats.Inserted++
		}
	}
	if err := tx.Commit(); err != nil {
		return BackfillStats{}, err
	}
	return stats, nil
}

func RunDaemon(ctx context.Context, opts Options) error {
	interval := opts.Interval
	if interval <= 0 {
		interval = defaultInterval
	}
	if interval < time.Second {
		interval = time.Second
	}
	if _, err := RunOnce(ctx, opts); err != nil && !errors.Is(err, ErrAlreadyRunning) {
		return err
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if _, err := RunOnce(ctx, opts); err != nil && !errors.Is(err, ErrAlreadyRunning) {
				return err
			}
		}
	}
}

func knownEventIDs(ctx context.Context, db *sql.DB) (map[string]struct{}, error) {
	rows, err := db.QueryContext(ctx, `SELECT id FROM events`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]struct{}{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = struct{}{}
	}
	return out, rows.Err()
}

func newEvents(ctx context.Context, db *sql.DB, before map[string]struct{}) ([]eventRow, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, kind, COALESCE(project, ''), COALESCE(thread_id, ''), COALESCE(body, ''), raw_json, created_at
FROM events
ORDER BY created_at ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []eventRow
	for rows.Next() {
		var row eventRow
		if err := rows.Scan(&row.ID, &row.Kind, &row.Project, &row.ThreadID, &row.Body, &row.RawJSON, &row.CreatedAt); err != nil {
			return nil, err
		}
		if _, ok := before[row.ID]; ok {
			continue
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func routeCounts(ctx context.Context, db *sql.DB, events []eventRow) (int, int, error) {
	ids := make([]string, 0, len(events))
	for _, event := range events {
		ids = append(ids, event.ID)
	}
	agent, err := countRowsForEvents(ctx, db, "agent_inbox", ids)
	if err != nil {
		return 0, 0, err
	}
	global, err := countRowsForEvents(ctx, db, "global_feed", ids)
	if err != nil {
		return 0, 0, err
	}
	return agent, global, nil
}

func countRowsForEvents(ctx context.Context, db *sql.DB, table string, eventIDs []string) (int, error) {
	count := 0
	for _, eventID := range eventIDs {
		var n int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+` WHERE event_id=?`, eventID).Scan(&n); err != nil {
			return 0, err
		}
		count += n
	}
	return count, nil
}

func enqueueMemorySync(ctx context.Context, db *sql.DB, events []eventRow) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, event := range events {
		if !syncableKind(event.Kind) {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO memory_sync(event_id, status)
VALUES (?, 'pending')
ON CONFLICT(event_id) DO NOTHING`, event.ID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func syncPending(ctx context.Context, opts Options) (int, int, error) {
	batchSize := opts.BatchSize
	if batchSize <= 0 {
		batchSize = defaultBatchSize
	}
	maxRetries := opts.MaxRetries
	if maxRetries <= 0 {
		maxRetries = defaultMaxRetries
	}
	rows, err := opts.StateDB.QueryContext(ctx, `SELECT ms.event_id, e.kind, COALESCE(e.project, ''), COALESCE(e.thread_id, ''), COALESCE(e.body, ''), e.raw_json, e.created_at
FROM memory_sync ms
JOIN events e ON e.id = ms.event_id
WHERE ms.status IN ('pending', 'failed') AND ms.retry_count < ?
ORDER BY ms.retry_count ASC, ms.event_id ASC
LIMIT ?`, maxRetries, batchSize)
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()
	var events []eventRow
	for rows.Next() {
		var event eventRow
		if err := rows.Scan(&event.ID, &event.Kind, &event.Project, &event.ThreadID, &event.Body, &event.RawJSON, &event.CreatedAt); err != nil {
			return 0, 0, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return 0, 0, err
	}
	var succeeded, failed int
	for _, event := range events {
		req := memoryRequest(event)
		resp, err := opts.Sync.WriteMemory(ctx, req)
		now := time.Now().UTC().Format(time.RFC3339)
		if err == nil {
			if _, updateErr := opts.StateDB.ExecContext(ctx, `UPDATE memory_sync
SET status='synced', synced_at=?, retry_count=retry_count, last_error=NULL, openviking_id=?
WHERE event_id=?`, now, nullString(resp.MemoryID), event.ID); updateErr != nil {
				return succeeded, failed, updateErr
			}
			succeeded++
			continue
		}
		retryCount, updateErr := markSyncFailed(ctx, opts.StateDB, event.ID, err.Error())
		if updateErr != nil {
			return succeeded, failed, updateErr
		}
		if retryCount >= maxRetries {
			if _, updateErr := opts.StateDB.ExecContext(ctx, `UPDATE memory_sync SET status='skipped' WHERE event_id=?`, event.ID); updateErr != nil {
				return succeeded, failed, updateErr
			}
		}
		failed++
	}
	return succeeded, failed, nil
}

func markSyncFailed(ctx context.Context, db *sql.DB, eventID, message string) (int, error) {
	if _, err := db.ExecContext(ctx, `UPDATE memory_sync
SET status='failed', retry_count=retry_count+1, last_error=?
WHERE event_id=?`, message, eventID); err != nil {
		return 0, err
	}
	var retryCount int
	if err := db.QueryRowContext(ctx, `SELECT retry_count FROM memory_sync WHERE event_id=?`, eventID).Scan(&retryCount); err != nil {
		return 0, err
	}
	return retryCount, nil
}

func refreshThreadSummaries(ctx context.Context, db *sql.DB, events []eventRow) (int, error) {
	touched := map[string]struct{}{}
	for _, event := range events {
		if strings.TrimSpace(event.ThreadID) != "" {
			touched[event.ThreadID] = struct{}{}
		}
	}
	threadIDs := make([]string, 0, len(touched))
	for threadID := range touched {
		threadIDs = append(threadIDs, threadID)
	}
	sort.Strings(threadIDs)
	updated := 0
	for _, threadID := range threadIDs {
		summary, sourceEventID, err := buildThreadSummary(ctx, db, threadID)
		if err != nil {
			return updated, err
		}
		now := time.Now().UTC().Format(time.RFC3339)
		id := "thread:" + threadID
		if _, err := db.ExecContext(ctx, `INSERT INTO summaries(id, scope, scope_id, summary, source_event_id, updated_at)
VALUES (?, 'thread', ?, ?, ?, ?)
ON CONFLICT(scope, scope_id) DO UPDATE SET
  summary=excluded.summary,
  source_event_id=excluded.source_event_id,
  updated_at=excluded.updated_at`, id, threadID, summary, nullString(sourceEventID), now); err != nil {
			return updated, err
		}
		updated++
	}
	return updated, nil
}

func buildThreadSummary(ctx context.Context, db *sql.DB, threadID string) (string, string, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, kind, COALESCE(from_agent, ''), COALESCE(body, ''), created_at
FROM events
WHERE thread_id=?
ORDER BY created_at DESC, id DESC
LIMIT 10`, threadID)
	if err != nil {
		return "", "", err
	}
	defer rows.Close()
	type part struct {
		id      string
		kind    string
		from    string
		body    string
		created string
	}
	var parts []part
	for rows.Next() {
		var p part
		if err := rows.Scan(&p.id, &p.kind, &p.from, &p.body, &p.created); err != nil {
			return "", "", err
		}
		parts = append(parts, p)
	}
	if err := rows.Err(); err != nil {
		return "", "", err
	}
	if len(parts) == 0 {
		return "", "", nil
	}
	var b strings.Builder
	for i := len(parts) - 1; i >= 0; i-- {
		p := parts[i]
		body := strings.Join(strings.Fields(p.body), " ")
		if body == "" {
			body = "(no body)"
		}
		fmt.Fprintf(&b, "[%s] %s from %s: %s\n", p.created, p.kind, fallback(p.from, "unknown"), body)
	}
	summary := b.String()
	if len(summary) > maxSummaryBytes {
		summary = summary[:maxSummaryBytes]
	}
	return strings.TrimSpace(summary), parts[0].id, nil
}

func memoryRequest(event eventRow) WriteMemoryRequest {
	content := strings.TrimSpace(event.Body)
	if content == "" {
		content = event.RawJSON
	}
	content = fmt.Sprintf("event:%s\nkind:%s\nthread:%s\n\n%s", event.ID, event.Kind, event.ThreadID, content)
	return WriteMemoryRequest{
		ProjectID:      event.Project,
		Target:         "summary",
		Title:          event.Kind + ":" + event.ID,
		Content:        content,
		RelatedEventID: event.ID,
		RelatedTaskID:  relatedTaskID(event.RawJSON),
	}
}

func relatedTaskID(raw string) string {
	var payload struct {
		TaskID string `json:"taskId"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.TaskID)
}

func syncableKind(kind string) bool {
	kind = strings.TrimSpace(kind)
	switch kind {
	case "broadcast",
		"context.handoff_created",
		"context_handoff",
		"memory_note",
		"mention",
		"note",
		"reply",
		"room.decision_pinned",
		"room.message",
		"room_message",
		"summary",
		"system_event",
		"task.delegated",
		"task.failed",
		"task.review_passed",
		"task.review_rejected",
		"task.review_task_created",
		"task.reviewed",
		"task.started",
		"task.submitted",
		"task.updated",
		"task_delegated",
		"task_done",
		"task_updated":
		return true
	default:
		return false
	}
}

func logStats(opts Options, stats Stats) {
	logger := opts.Logger
	if logger == nil {
		logger = log.Default()
	}
	logger.Printf("worker stats: ingested=%d routed_agent=%d routed_global=%d sync_succeeded=%d sync_failed=%d summaries_updated=%d",
		stats.Ingested, stats.RoutedAgent, stats.RoutedGlobal, stats.SyncSucceeded, stats.SyncFailed, stats.SummariesUpdated)
}

func acquireLock(opts Options) (func(), error) {
	if skipLock(opts) {
		return func() {}, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	lockPath := filepath.Join(home, ".aitask", "runtime", "worker.lock")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrAlreadyRunning
		}
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
	}, nil
}

func skipLock(opts Options) bool {
	if strings.HasPrefix(filepath.Clean(opts.NDJSONPath), filepath.Clean(os.TempDir())+string(os.PathSeparator)) {
		return true
	}
	if opts.Logger != nil && strings.Contains(opts.Logger.Prefix(), "test") {
		return true
	}
	return false
}

func nullString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return strings.TrimSpace(value)
}

func fallback(value, replacement string) string {
	if strings.TrimSpace(value) == "" {
		return replacement
	}
	return strings.TrimSpace(value)
}

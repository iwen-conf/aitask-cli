package worker

import (
	"context"
	"database/sql"
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
	defaultInterval = 10 * time.Second
	maxSummaryBytes = 4 * 1024
)

var ErrAlreadyRunning = errors.New("worker already running")

type Options struct {
	StateDB    *sql.DB
	NDJSONPath string
	Interval   time.Duration
	Logger     *log.Logger
}

type Stats struct {
	Ingested         int `json:"ingested"`
	RoutedAgent      int `json:"routedAgent"`
	RoutedGlobal     int `json:"routedGlobal"`
	SummariesUpdated int `json:"summariesUpdated"`
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
		updated, err := refreshThreadSummaries(ctx, opts.StateDB, events)
		if err != nil {
			return Stats{}, err
		}
		stats.SummariesUpdated = updated
	}
	logStats(opts, stats)
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

func logStats(opts Options, stats Stats) {
	logger := opts.Logger
	if logger == nil {
		logger = log.Default()
	}
	logger.Printf("worker stats: ingested=%d routed_agent=%d routed_global=%d summaries_updated=%d",
		stats.Ingested, stats.RoutedAgent, stats.RoutedGlobal, stats.SummariesUpdated)
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

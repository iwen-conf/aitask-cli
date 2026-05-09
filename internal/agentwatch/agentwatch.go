package agentwatch

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	localinbox "github.com/iwen-conf/aitask-cli/internal/inbox"
)

const (
	defaultInterval   = 5 * time.Second
	defaultTimeout    = 5 * time.Minute
	defaultMaxRetries = 5
	defaultMaxBatch   = 10
)

var ErrAlreadyRunning = errors.New("agent watcher already running")

type Options struct {
	StateDB         *sql.DB
	Agent           string
	Runner          Runner
	ContextRecaller ContextRecaller
	Once            bool
	Interval        time.Duration
	Timeout         time.Duration
	DryRun          bool
	Logger          *log.Logger
	MaxRetries      int
	MaxBatch        int
	PromptWriter    io.Writer
}

type Runner interface {
	Run(ctx context.Context, prompt string) (RunResult, error)
}

type RunResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

type ContextRecaller interface {
	Recall(ctx context.Context, agent, eventID string) (string, error)
}

type Stats struct {
	Picked  int `json:"picked"`
	Acked   int `json:"acked"`
	Done    int `json:"done"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
}

func RunOnce(ctx context.Context, opts Options) (Stats, error) {
	if opts.StateDB == nil {
		return Stats{}, fmt.Errorf("state db is required")
	}
	agent := strings.TrimSpace(opts.Agent)
	if agent == "" {
		return Stats{}, fmt.Errorf("agent is required")
	}
	if !opts.DryRun && opts.Runner == nil {
		return Stats{}, fmt.Errorf("runner is required")
	}
	release, err := acquireLock(ctx, opts)
	if err != nil {
		return Stats{}, err
	}
	defer release()

	maxBatch := opts.MaxBatch
	if maxBatch <= 0 {
		maxBatch = defaultMaxBatch
	}
	rows, err := localinbox.ListAgentInbox(ctx, opts.StateDB, agent, localinbox.ListOpts{Status: "unread,seen,acked", Limit: maxBatch})
	if err != nil {
		return Stats{}, err
	}
	stats := Stats{}
	for _, row := range rows {
		if !shouldProcess(row, opts) {
			continue
		}
		stats.Picked++
		if opts.DryRun {
			prompt, err := renderRowPrompt(ctx, opts, row)
			if err != nil {
				return stats, err
			}
			writePrompt(opts, prompt)
			return stats, nil
		}
		ackedNow := false
		if row.Status != "acked" {
			if _, err := localinbox.Ack(ctx, opts.StateDB, row.ID, agent); err != nil {
				if errors.Is(err, localinbox.ErrNotApplicable) {
					continue
				}
				return stats, err
			}
			stats.Acked++
			ackedNow = true
		}
		prompt, err := renderRowPrompt(ctx, opts, row)
		if err != nil {
			return stats, err
		}
		result, runErr := runWithTimeout(ctx, opts, prompt)
		if runErr == nil && result.ExitCode == 0 {
			if _, err := localinbox.Done(ctx, opts.StateDB, row.ID, agent); err != nil {
				if errors.Is(err, localinbox.ErrNotApplicable) {
					continue
				}
				return stats, err
			}
			if err := persistRunResult(ctx, opts.StateDB, row, agent, result); err != nil {
				logger(opts).Printf("persist run result failed for %s: %v", row.ID, err)
			}
			stats.Done++
			_ = updateCursor(ctx, opts.StateDB, agent, row.ID)
			continue
		}
		message := failureMessage(result, runErr)
		if _, err := localinbox.Fail(ctx, opts.StateDB, row.ID, agent, message); err != nil {
			if errors.Is(err, localinbox.ErrNotApplicable) && ackedNow {
				continue
			}
			return stats, err
		}
		stats.Failed++
		if shouldSkipAfterFailure(row, opts) {
			if _, err := localinbox.Skip(ctx, opts.StateDB, row.ID, agent, "exceeded MaxRetries: "+message); err != nil {
				if errors.Is(err, localinbox.ErrNotApplicable) {
					continue
				}
				return stats, err
			}
			stats.Skipped++
		}
		_ = updateCursor(ctx, opts.StateDB, agent, row.ID)
	}
	return stats, nil
}

func RunLoop(ctx context.Context, opts Options) error {
	interval := opts.Interval
	if interval <= 0 {
		interval = defaultInterval
	}
	if interval < time.Second {
		interval = time.Second
	}
	if _, err := RunOnce(ctx, opts); err != nil {
		return err
	}
	if opts.Once {
		return nil
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if _, err := RunOnce(ctx, opts); err != nil {
				return err
			}
		}
	}
}

func LoadPromptEvent(ctx context.Context, db *sql.DB, eventID string) (PromptEvent, error) {
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return PromptEvent{}, fmt.Errorf("event id is required")
	}
	var evt PromptEvent
	var project, threadID, fromAgent, body sql.NullString
	err := db.QueryRowContext(ctx, `SELECT id, kind, COALESCE(project, ''), COALESCE(thread_id, ''), COALESCE(from_agent, ''), COALESCE(body, ''), created_at
FROM events
WHERE id = ?`, eventID).Scan(&evt.ID, &evt.Kind, &project, &threadID, &fromAgent, &body, &evt.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return PromptEvent{}, fmt.Errorf("event %s not found", eventID)
		}
		return PromptEvent{}, err
	}
	evt.Project = project.String
	evt.ThreadID = threadID.String
	evt.From = fromAgent.String
	evt.Body = body.String
	return evt, nil
}

func shouldProcess(row localinbox.InboxRow, opts Options) bool {
	switch row.Status {
	case "unread", "seen":
		return true
	case "acked":
		return isStaleAck(row, opts)
	default:
		return false
	}
}

func isStaleAck(row localinbox.InboxRow, opts Options) bool {
	threshold := opts.Timeout
	if threshold <= 0 {
		threshold = defaultTimeout
	}
	threshold *= 10
	if strings.TrimSpace(row.AckedAt) == "" {
		return true
	}
	ackedAt, err := time.Parse(time.RFC3339, row.AckedAt)
	if err != nil {
		return true
	}
	return time.Since(ackedAt) >= threshold
}

func renderRowPrompt(ctx context.Context, opts Options, row localinbox.InboxRow) (string, error) {
	recall := ""
	if opts.ContextRecaller != nil {
		value, err := opts.ContextRecaller.Recall(ctx, opts.Agent, row.ID)
		if err != nil {
			logger(opts).Printf("context recall failed for %s: %v", row.ID, err)
		} else {
			recall = value
		}
	}
	evt := PromptEvent{ID: row.ID, Kind: row.Kind, From: row.FromAgent, Project: row.Project, ThreadID: row.ThreadID, CreatedAt: row.CreatedAt, Body: row.Body}
	return RenderPrompt(row.ID, opts.Agent, recall, evt), nil
}

func runWithTimeout(ctx context.Context, opts Options, prompt string) (RunResult, error) {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return opts.Runner.Run(runCtx, prompt)
}

func shouldSkipAfterFailure(row localinbox.InboxRow, opts Options) bool {
	maxRetries := opts.MaxRetries
	if maxRetries <= 0 {
		maxRetries = defaultMaxRetries
	}
	return row.RetryCount+1 >= maxRetries
}

func failureMessage(result RunResult, err error) string {
	parts := []string{}
	if err != nil {
		parts = append(parts, err.Error())
	}
	if strings.TrimSpace(result.Stderr) != "" {
		parts = append(parts, result.Stderr)
	}
	if len(parts) == 0 && result.ExitCode != 0 {
		parts = append(parts, fmt.Sprintf("runner exited with code %d", result.ExitCode))
	}
	message := strings.Join(parts, ": ")
	if strings.TrimSpace(message) == "" {
		message = "runner failed"
	}
	return tail(message, 2048)
}

func persistRunResult(ctx context.Context, db *sql.DB, row localinbox.InboxRow, agent string, result RunResult) error {
	content := semanticRunContent(result)
	if content == "" {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	eventID := strings.TrimSpace(row.ID) + ":result:" + safeLockName(agent)
	raw, err := json.Marshal(map[string]any{
		"kind":            "task_done",
		"ts":              now,
		"project":         strings.TrimSpace(row.Project),
		"eventId":         eventID,
		"parentEventId":   strings.TrimSpace(row.ID),
		"messageId":       strings.TrimSpace(row.ThreadID),
		"content":         content,
		"assigneeAgentId": strings.TrimSpace(agent),
		"from": map[string]any{
			"type":      "agent",
			"agentType": strings.TrimSpace(agent),
		},
		"details": map[string]any{
			"source":        "aitask watch --agent",
			"originalEvent": strings.TrimSpace(row.ID),
			"exitCode":      result.ExitCode,
		},
	})
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO events(id, kind, scope, project, thread_id, from_agent, to_agent, body, raw_json, created_at, indexed_at)
VALUES (?, 'task_done', ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  kind=excluded.kind, scope=excluded.scope, project=excluded.project, thread_id=excluded.thread_id,
  from_agent=excluded.from_agent, to_agent=excluded.to_agent, body=excluded.body, raw_json=excluded.raw_json,
  created_at=excluded.created_at, indexed_at=excluded.indexed_at`,
		eventID, nullString(row.Scope), nullString(row.Project), nullString(row.ThreadID), nullString(agent), nullString(row.FromAgent), content, string(raw), now, now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO memory_sync(event_id, status)
VALUES (?, 'pending')
ON CONFLICT(event_id) DO NOTHING`, eventID); err != nil {
		return err
	}
	return tx.Commit()
}

func semanticRunContent(result RunResult) string {
	content := strings.TrimSpace(result.Stdout)
	if content == "" {
		return ""
	}
	if len(content) > 16*1024 {
		content = content[:16*1024] + "\n\n[truncated by aitask-agent-watch]"
	}
	return content
}

func writePrompt(opts Options, prompt string) {
	if opts.PromptWriter != nil {
		_, _ = fmt.Fprintln(opts.PromptWriter, prompt)
		return
	}
	logger(opts).Print(prompt)
}

func updateCursor(ctx context.Context, db *sql.DB, agent, eventID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.ExecContext(ctx, `INSERT INTO cursors(consumer, source, offset, event_id, updated_at)
VALUES (?, 'state.db', 0, ?, ?)
ON CONFLICT(consumer) DO UPDATE SET source=excluded.source, event_id=excluded.event_id, updated_at=excluded.updated_at`,
		"agent-watch:"+strings.TrimSpace(agent), strings.TrimSpace(eventID), now)
	return err
}

func acquireLock(ctx context.Context, opts Options) (func(), error) {
	if skipLock(ctx, opts) {
		return func() {}, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	lockPath := filepath.Join(home, ".aitask", "runtime", "agent-watch", safeLockName(opts.Agent)+".lock")
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

func skipLock(ctx context.Context, opts Options) bool {
	if opts.Logger != nil && strings.Contains(opts.Logger.Prefix(), "test") {
		return true
	}
	path, err := databasePath(ctx, opts.StateDB)
	if err != nil || path == "" || path == ":memory:" {
		return false
	}
	clean := filepath.Clean(path)
	tmp := filepath.Clean(os.TempDir()) + string(os.PathSeparator)
	return strings.HasPrefix(clean, tmp)
}

func databasePath(ctx context.Context, db *sql.DB) (string, error) {
	rows, err := db.QueryContext(ctx, `PRAGMA database_list`)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var seq int
		var name, file string
		if err := rows.Scan(&seq, &name, &file); err != nil {
			return "", err
		}
		if name == "main" {
			return file, nil
		}
	}
	return "", rows.Err()
}

func logger(opts Options) *log.Logger {
	if opts.Logger != nil {
		return opts.Logger
	}
	return log.Default()
}

func safeLockName(agent string) string {
	agent = strings.TrimSpace(agent)
	if agent == "" {
		return "default"
	}
	var b strings.Builder
	for _, r := range agent {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('_')
	}
	return b.String()
}

func tail(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[len(value)-limit:]
}

func nullString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return strings.TrimSpace(value)
}

package state

import (
	"context"
	"database/sql"
)

const schemaVersion = "2"

func Migrate(ctx context.Context, db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS events (
  id          TEXT PRIMARY KEY,
  kind        TEXT NOT NULL,
  scope       TEXT,
  project     TEXT,
  thread_id   TEXT,
  from_agent  TEXT,
  to_agent    TEXT,
  body        TEXT,
  raw_json    TEXT NOT NULL,
  created_at  TEXT NOT NULL,
  indexed_at  TEXT NOT NULL
)`,
		`CREATE INDEX IF NOT EXISTS idx_events_created_at ON events(created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_events_to_agent ON events(to_agent)`,
		`CREATE INDEX IF NOT EXISTS idx_events_thread ON events(thread_id)`,
		`CREATE INDEX IF NOT EXISTS idx_events_project ON events(project)`,
		`CREATE TABLE IF NOT EXISTS agent_inbox (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  event_id      TEXT NOT NULL,
  agent         TEXT NOT NULL,
  status        TEXT NOT NULL DEFAULT 'unread',
  seen_at       TEXT,
  acked_at      TEXT,
  handled_at    TEXT,
  failed_at     TEXT,
  retry_count   INTEGER NOT NULL DEFAULT 0,
  last_error    TEXT,
  UNIQUE(event_id, agent),
  FOREIGN KEY(event_id) REFERENCES events(id)
)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_inbox_agent_status ON agent_inbox(agent, status)`,
		`CREATE TABLE IF NOT EXISTS global_feed (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  event_id    TEXT NOT NULL UNIQUE,
  project     TEXT,
  visibility  TEXT NOT NULL DEFAULT 'project',
  wake        INTEGER NOT NULL DEFAULT 0,
  created_at  TEXT NOT NULL,
  FOREIGN KEY(event_id) REFERENCES events(id)
)`,
		`CREATE INDEX IF NOT EXISTS idx_global_feed_created_at ON global_feed(created_at)`,
		`CREATE TABLE IF NOT EXISTS cursors (
  consumer    TEXT PRIMARY KEY,
  source      TEXT NOT NULL,
  offset      INTEGER NOT NULL DEFAULT 0,
  event_id    TEXT,
  updated_at  TEXT NOT NULL
)`,
		`CREATE TABLE IF NOT EXISTS memory_sync (
  event_id      TEXT PRIMARY KEY,
  status        TEXT NOT NULL DEFAULT 'pending',
  synced_at     TEXT,
  retry_count   INTEGER NOT NULL DEFAULT 0,
  last_error    TEXT,
  openviking_id TEXT,
  FOREIGN KEY(event_id) REFERENCES events(id)
)`,
		`CREATE TABLE IF NOT EXISTS summaries (
  id              TEXT PRIMARY KEY,
  scope           TEXT NOT NULL,
  scope_id        TEXT NOT NULL,
  summary         TEXT,
  source_event_id TEXT,
  updated_at      TEXT NOT NULL,
  memory_id       TEXT,
  UNIQUE(scope, scope_id)
)`,
		`CREATE TABLE IF NOT EXISTS schema_meta (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
)`,
		`INSERT INTO schema_meta(key, value) VALUES ('schema_version', '` + schemaVersion + `')
ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

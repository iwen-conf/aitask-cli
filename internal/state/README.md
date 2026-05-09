# Local Runtime State

This directory defines the local SQLite state layer shared by inbox, worker, hooks, and agent-watch.

## Directory Layout

```text
~/.aitask/
├── config.json
├── events.ndjson
├── hook-state/
│   ├── claude-prompt-offset
│   ├── codex-prompt-offset
│   └── gemini-prompt-offset
├── state.db
├── memory-cache/
└── runtime/
    └── tmux/
```

`AITASK_STATE_DB` can override the default `~/.aitask/state.db` path. The store uses SQLite WAL with `PRAGMA journal_mode=WAL` and `PRAGMA synchronous=NORMAL`.

## events.ndjson

- Each line is one JSON event.
- Required fields: `id`, `kind`, `created_at`.
- Recommended fields: `scope`, `project`, `thread_id`, `from`, `to`, `body`, `wake`, `metadata`.
- Common kinds: `mention`, `task_delegated`, `broadcast`, `task_done`, `summary`, `note`, `ready`, `error`, `daemon`, `connect`, `disconnect`.
- Writer: `aitask-watch`.
- Consumers must handle truncation or rotation by resetting stale byte offsets.

## state.db Schema

### events

Normalized event copy.

```sql
CREATE TABLE events (
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
);
CREATE INDEX idx_events_created_at ON events(created_at);
CREATE INDEX idx_events_to_agent ON events(to_agent);
CREATE INDEX idx_events_thread ON events(thread_id);
CREATE INDEX idx_events_project ON events(project);
```

### agent_inbox

Per-Agent inbox projection.

```sql
CREATE TABLE agent_inbox (
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
);
CREATE INDEX idx_agent_inbox_agent_status ON agent_inbox(agent, status);
```

Status values:

```text
unread -> seen -> acked -> handled
                  |
                  +-> failed
                  +-> skipped
```

`handled` and `skipped` are terminal states. `agent-watch` must ack before invoking a runner.

### global_feed

Broadcast and project-level feed.

```sql
CREATE TABLE global_feed (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  event_id    TEXT NOT NULL UNIQUE,
  project     TEXT,
  visibility  TEXT NOT NULL DEFAULT 'project',
  wake        INTEGER NOT NULL DEFAULT 0,
  created_at  TEXT NOT NULL,
  FOREIGN KEY(event_id) REFERENCES events(id)
);
CREATE INDEX idx_global_feed_created_at ON global_feed(created_at);
```

### cursors

Consumer-private offsets.

```sql
CREATE TABLE cursors (
  consumer    TEXT PRIMARY KEY,
  source      TEXT NOT NULL,
  offset      INTEGER NOT NULL DEFAULT 0,
  event_id    TEXT,
  updated_at  TEXT NOT NULL
);
```

Known consumers:

```text
worker:indexer
worker:openviking
worker:summarizer
hook:claude-code
hook:codex
hook:gemini
agent-watch:claude-code
agent-watch:codex
agent-watch:gemini
```

### memory_sync

OpenViking sync queue.

```sql
CREATE TABLE memory_sync (
  event_id      TEXT PRIMARY KEY,
  status        TEXT NOT NULL DEFAULT 'pending',
  synced_at     TEXT,
  retry_count   INTEGER NOT NULL DEFAULT 0,
  last_error    TEXT,
  openviking_id TEXT,
  FOREIGN KEY(event_id) REFERENCES events(id)
);
```

### summaries

Local summary cache.

```sql
CREATE TABLE summaries (
  id              TEXT PRIMARY KEY,
  scope           TEXT NOT NULL,
  scope_id        TEXT NOT NULL,
  summary         TEXT,
  source_event_id TEXT,
  updated_at      TEXT NOT NULL,
  memory_id       TEXT,
  UNIQUE(scope, scope_id)
);
```

### schema_meta

```sql
CREATE TABLE schema_meta (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
```

`schema_version` is currently `2`.

## Concurrency Rules

- Use short transactions.
- Batch ingest in bounded groups.
- Prefer idempotent `INSERT OR IGNORE` / UPSERT.
- State mutators use compare-and-set style `UPDATE ... WHERE status IN (...)`.
- Do not store cursor, ack, retry, or handled state in OpenViking.

## Compatibility

- If `state.db` is missing, query commands may do a one-shot read-only ingest from `events.ndjson`.
- Status commands require a writable `state.db`.
- If neither `state.db` nor `events.ndjson` exists, query commands return empty results.
- Existing Context Injection Mode keeps reading `events.ndjson` and does not require `state.db`.

## Test Rules

- Tests use `t.TempDir()` and `AITASK_STATE_DB`.
- Tests must not touch the real user `~/.aitask/state.db`.
- Migration tests assert all core tables exist.

[AITask Phase 2 — Local inbox + SQLite state.db]

Repository: /Users/iluwen/Documents/Code/Lazycat/Projects/aitask
Authoritative spec docs to read in this order:
  1. 改造说明.md (sections 1–9, 17, 18)
  2. cli/internal/state/README.md (schema + state file layout)
  3. cli/internal/cli/aitask-watch.md (mode boundaries — must keep Context Injection Mode untouched)
  4. cli/internal/inbox/skills/SKILL.md (this skill is the contract)
  5. cli/internal/cli/app.go + cli/internal/cli/command_events.go (cobra wiring + Printer/RenderData/format conventions)
  6. cli/internal/cli/state.go + state_cache.go (existing local file conventions)
  7. cli/go.mod (no SQLite driver yet — add modernc.org/sqlite, pure Go, no CGO)

Goal: deliver Phase 2 of the refactor. Output format MUST be a unified diff only — do NOT write to disk.
Sandbox is read-only. Keep the diff minimal, idempotent, and testable.

Deliverables:

A. New package `cli/internal/state`
   - File `state.go`: StoreOpener that resolves path from
       env AITASK_STATE_DB, otherwise filepath.Join(homeDir, ".aitask", "state.db").
       Opens via modernc.org/sqlite with PRAGMA journal_mode=WAL; synchronous=NORMAL.
       Returns *sql.DB plus a Close.
   - File `migrate.go`: idempotent migrations creating tables exactly as in
     cli/internal/state/README.md schema sections (events, agent_inbox, global_feed, cursors)
     plus indexes. Use `CREATE TABLE IF NOT EXISTS` and a `schema_version` row
     in cursors-like meta table or a dedicated `schema_meta(key TEXT PRIMARY KEY, value TEXT)`.
   - Tests in `state_test.go` using t.TempDir() + AITASK_STATE_DB override.

B. New package `cli/internal/inbox`
   Public funcs (all return concrete typed structs, not map[string]any):
     - Ingest(ctx, db, ndjsonPath string) error   // idempotent UPSERT from events.ndjson; uses cursors row consumer="worker:indexer" but DOES NOT spawn a daemon; safe to call from CLI
     - ListAgentInbox(ctx, db, agent string, opts ListOpts) ([]InboxRow, error)
     - ListGlobalFeed(ctx, db, opts ListOpts) ([]InboxRow, error)
     - ListLatest(ctx, db, limit int) ([]EventRow, error)
     - ListThread(ctx, db, threadID string) ([]EventRow, error)
     - Ack/Done/Fail/Skip(ctx, db, eventID, agent string, extra ...) error
   Behaviour requirements:
     - ListAgentInbox MUST exclude rows where events.from_agent == agent (do not show self-sent).
     - Default status filter: unread,seen,acked. opts.AllStatuses=true returns all.
     - All status mutators use UPDATE … WHERE status IN (allowed_prev_states) (CAS); affected==0 → typed error ErrNotApplicable.
     - State machine matches cli/internal/state/README.md status machine.
   Tests: state_test-style with seeded NDJSON and a tmp DB; cover happy path, double-ack, self-mention exclusion, missing event_id.

C. CLI wiring (cli/internal/cli)
   - New file `command_inbox.go` registering:
       inbox  (with --agent string, --global bool, --status string, --limit int)
       latest (with --limit int)
       thread <thread_id>
       ack/done/fail/skip <event_id> (--agent, --reason, --error)
     The status sub-commands accept --agent or fall back to current profile resolveProfile.
   - `app.go`: AddCommand the new commands alongside existing newEventsCommand etc.
   - All output goes through env.printer().Print(RenderData{Brief,Prompt,JSON}). JSON branch returns the raw struct slice.
   - On state.db missing AND events.ndjson present, query commands MUST do a one-shot in-memory ingest and answer; status commands must require state.db (no read-only fallback).
   - Respect existing global flags (--format, --timeout). No network calls.

D. Tests
   - cli/internal/inbox/*_test.go — unit tests as above.
   - cli/internal/cli/command_inbox_test.go — table-driven tests using NewApp + bytes.Buffer for Stdout. Cover:
       1. inbox --agent claude-code (with seeded NDJSON, no state.db) returns expected items
       2. inbox --global returns broadcast/global rows only
       3. latest --limit 2 sorts DESC
       4. ack twice → second errors with ErrNotApplicable
       5. self-mention excluded
   - Keep go test ./... green.

E. Hard constraints (must respect)
   - DO NOT touch scripts/aitask-hooks/* nor cli/internal/cli/command_events.go beyond imports.
   - DO NOT change Context Injection Mode behavior.
   - DO NOT add OpenViking calls in this phase.
   - DO NOT add background goroutines / daemons.
   - DO NOT add CGO. Use modernc.org/sqlite.
   - Keep diff < 2500 LoC where possible.
   - Comments only when WHY is non-obvious.

Return: a single unified diff (against current main) plus a one-paragraph summary at top in the diff cover letter. No file writes.

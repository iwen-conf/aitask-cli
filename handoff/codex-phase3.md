[AITask Phase 3 — aitask-worker (sync via backend, NOT direct OpenViking)]

Repository: /Users/iluwen/Documents/Code/Lazycat/Projects/aitask
Sandbox: read-only. Output: unified diff only against current main.

Direction correction (mandatory): the previous draft of Phase 3 told you to write a
new openviking direct-HTTP package and brand-new top-level `aitask search/context/summary`
commands. That was wrong — it duplicates wheels already in the repo:
  - `aitask memory search/read/write` (cli/internal/cli/command_memory.go) already proxies
    OpenViking through the AITask backend `/api/projects/:projectId/memory/*`.
  - `aitask context status/report/compact/handoff` (cli/internal/cli/command_context.go)
    already exists and would collide with `aitask context --event ...`.
So Phase 3 reuses the existing backend client; do NOT add a direct-OV package and do
NOT add new top-level search/context/summary commands.

Authoritative spec (read in this order):
  1. 改造说明.md sections 6, 10, 17 (deliverables + acceptance)
  2. integrations/openviking/README.md (boundary; state stays in state.db)
  3. cli/internal/state/README.md sections for memory_sync and summaries
  4. cli/internal/worker/skills/SKILL.md (contract)
  5. cli/internal/inbox/inbox.go (existing Ingest — extend, do not duplicate)
  6. cli/internal/state/migrate.go (idempotent migrations)
  7. cli/internal/cli/command_memory.go (the sync target — reuse this client/REST shape)
  8. cli/internal/cli/client.go (existing REST client + token plumbing)
  9. cli/internal/cli/command_inbox.go (mirror its Printer/RenderData/json output style)

Hard constraints:
  - Pure Go, no CGO. Reuse modernc.org/sqlite already in go.mod.
  - Do NOT add new direct-OV HTTP package. Do NOT add `aitask search`, `aitask context --event/--thread`, `aitask summary` top-level commands.
  - Worker syncs via the existing AITask backend: POST /api/projects/:projectId/memory/write (same as `aitask memory write`).
  - Backend failure must NOT block events ingest or inbox queries.
  - All inserts/updates idempotent; safe to replay events.ndjson from offset 0.
  - Persist offset + last event_id atomically with the batch.
  - No background goroutines outside `--daemon` mode.
  - No new external deps. No new top-level cobra commands beyond `worker`.
  - Do not touch scripts/aitask-hooks/.
  - Do not touch command_memory.go / command_context.go / command_inbox.go.

Deliverables:

A. cli/internal/state/migrate.go
   Add two tables (CREATE IF NOT EXISTS):
     memory_sync(event_id PK, status TEXT NOT NULL DEFAULT 'pending',
                 synced_at TEXT, retry_count INTEGER NOT NULL DEFAULT 0,
                 last_error TEXT, openviking_id TEXT, FOREIGN KEY(event_id) REFERENCES events(id))
     summaries(id TEXT PRIMARY KEY, scope TEXT NOT NULL, scope_id TEXT NOT NULL,
               summary TEXT, source_event_id TEXT, updated_at TEXT NOT NULL,
               memory_id TEXT, UNIQUE(scope, scope_id))
   Bump schema_meta.schema_version to '2'.
   Existing tests in state_test.go must still pass; add a test asserting both tables exist.

B. cli/internal/worker package (new)
   Public surface:
     type Options struct {
        StateDB    *sql.DB
        NDJSONPath string
        Sync       Syncer        // injected; nil = sync disabled
        Interval   time.Duration // for daemon
        BatchSize  int           // memory_sync batch
        MaxRetries int           // before status='skipped'
        Logger     *log.Logger   // optional
     }
     type Syncer interface {
        WriteMemory(ctx context.Context, req WriteMemoryRequest) (WriteMemoryResponse, error)
     }
     type WriteMemoryRequest struct { ProjectID, Target, Title, Content, RelatedEventID, RelatedTaskID string }
     type WriteMemoryResponse struct { URI, MemoryID string }
     type Stats struct { Ingested, RoutedAgent, RoutedGlobal, SyncSucceeded, SyncFailed, SummariesUpdated int }

     func RunOnce(ctx context.Context, opts Options) (Stats, error)
     func RunDaemon(ctx context.Context, opts Options) error  // ticker + ctx-cancel; respects opts.Interval (default 10s, min 1s)

   Pipeline (single tick):
     1. Try-acquire ~/.aitask/runtime/worker.lock via flock; on contention return ErrAlreadyRunning. Skip lock if NDJSONPath looks like a tmp-test path (e.g., contains os.TempDir() prefix) OR opts.Logger.Prefix says so — keep it test-friendly.
     2. Ingest events.ndjson from cursors row consumer='worker:indexer'. Reuse inbox.Ingest semantics; ensure offset advances by cumulative byte count (existing Ingest already does this; if not, fix it minimally without breaking inbox tests).
     3. For each newly-inserted event whose kind is in {mention,task_delegated,task_done,broadcast,summary,note,reply}, INSERT INTO memory_sync(event_id, status) VALUES (?, 'pending') ON CONFLICT DO NOTHING.
     4. If opts.Sync != nil:
          select up to BatchSize rows WHERE status IN ('pending','failed') ORDER BY retry_count ASC, event_id ASC.
          For each: render compact memory body (title = "evt:<event_id>" or "<kind>:<event_id>"; content = body or summary),
            call Sync.WriteMemory; on success → status='synced', openviking_id=resp.MemoryID, synced_at=now;
            on failure → status='failed', retry_count++, last_error=err.Error();
            if retry_count >= MaxRetries (default 5) → status='skipped'.
     5. Summary refresh (LocalConcatStrategy):
          For each thread_id touched in this tick: take latest <=10 events, concat bodies, truncate to 4KB, UPSERT summaries(scope='thread', scope_id=thread_id).
          If opts.Sync != nil and AutoSync (Options field; default false in this phase): also push summary as memory write target='summary'; capture memory_id back to summaries.memory_id.
     6. Update cursors row updated_at and offset.
   Telemetry: write final Stats line via opts.Logger when set, else stdlib log Default.
   Daemon mode: time.NewTicker(opts.Interval) + select on ctx.Done; trap SIGINT/SIGTERM at the CLI layer (not inside worker).

C. Backend-bound Syncer adapter (cli/internal/cli)
   Add a tiny adapter in a new file, e.g. cli/internal/cli/worker_syncer.go:
     - `type backendSyncer struct { client *Client; projectID string }`
     - `func (s *backendSyncer) WriteMemory(ctx, req) (resp, error)` — calls client.PostREST("/api/projects/<projectID>/memory/write", ...) with payload {target, title, content, relatedTaskId} and returns URI/MemoryID parsed from the existing response shape.
   Reuse client.go entirely. No new HTTP code outside this adapter.

D. CLI wiring
   Add cli/internal/cli/command_worker.go:
     - aitask worker [--once|--daemon] [--memory backend|none] [--interval 10s] [--batch 50] [--max-retries 5] [--quiet]
     - default: --once. --memory backend default; --memory none disables Sync (Sync = nil).
     - When --memory backend: build backendSyncer using env.clientWithToken(true) + resolveProjectConfig(true).
     - Wire single command in app.go:
         root.AddCommand(..., newWorkerCommand(env), ...)
     - Output: env.printer().Print(RenderData{...}) showing Stats; honor --format brief|prompt|json.

E. Tests
   - cli/internal/state/state_test.go: extend to cover memory_sync + summaries existence after Migrate.
   - cli/internal/worker/worker_test.go:
       * end-to-end RunOnce with seeded NDJSON + fake Syncer (in-process struct implementing the interface) — verify ingested counts, memory_sync transitions, summaries upserts.
       * Sync down (Syncer returns error): events still ingested; memory_sync rows go failed with retry_count++.
       * Replay: two RunOnce calls produce no duplicates (PK/UNIQUE).
       * --max-retries: status flips to 'skipped' after threshold.
       * Daemon: short Interval, cancel via ctx after 2 ticks; ensure no goroutine leaks (use t.Cleanup or explicit wait).
   - cli/internal/cli/command_worker_test.go: smoke test runs `worker --once --memory none` against HOME=t.TempDir; assert exit 0 and non-empty stats output.

F. What you must NOT do
   - Do NOT add cli/internal/openviking package.
   - Do NOT add `aitask search`, `aitask context --event`, `aitask context --thread`, `aitask summary` top-level commands.
   - Do NOT modify command_memory.go, command_context.go, command_inbox.go.
   - Do NOT add OpenViking config fields to ~/.aitask/config.json (backend already holds OV creds).
   - Do NOT add agent-watch / runner / wake logic (Phase 4).

Return: a single unified diff (against current main) plus a one-paragraph cover summary at the top. No file writes.

[AITask Phase 4 — agent-watch (Mailbox Worker Mode execution layer)]

Repository: /Users/iluwen/Documents/Code/Lazycat/Projects/aitask
Sandbox: read-only. Output: unified diff only against current main.

This is the final phase of the local-runtime refactor. Phase 2/3 already shipped:
state.db, inbox/ack/done/fail/skip, worker --once/--daemon syncing through backend.
Phase 4 adds the per-agent execution loop: pick up @-mentions for a specific agent,
optionally render a prompt and fire a one-shot runner, write the result back.

Authoritative spec (read in this order):
  1. 改造说明.md sections 11, 12, 17 (Phase 4 deliverables + acceptance)
  2. cli/internal/agentwatch/skills/SKILL.md (the contract — pay close attention to "不负责什么" and "Agent 使用注意事项")
  3. cli/internal/inbox/inbox.go (Ack/Done/Fail/Skip/ListAgentInbox already exist — REUSE, do not duplicate)
  4. cli/internal/worker/worker.go (mirror flock + Options pattern)
  5. cli/internal/cli/command_inbox.go and command_worker.go (mirror Printer/RenderData/cobra style)
  6. cli/internal/cli/command_memory.go (existing aitask memory search hits backend; reuse for context recall)
  7. cli/internal/cli/command_context.go (read-only; do NOT modify)

Hard constraints:
  - Pure Go, no CGO. No new external deps.
  - DO NOT do tmux send-keys / stdin injection into running REPLs. First version uses one-shot runners only (claude -p / codex exec / gemini).
  - DO NOT subscribe WebSocket. DO NOT write events.ndjson.
  - DO NOT modify command_memory.go, command_context.go, command_inbox.go (the inbox ack/done/fail/skip CLI is already there — call the inbox package directly from agentwatch).
  - DO NOT process events where from_agent equals the watcher agent (already enforced by ListAgentInbox; preserve that behavior).
  - Preserve Context Injection Mode (scripts/aitask-hooks/*) untouched.
  - State machine MUST go through the existing CAS path in inbox.Ack/Done/Fail/Skip. Do NOT write SQL UPDATEs directly.
  - Idempotent: a watcher restart must not double-process events.
  - Backend / OpenViking failure during context recall must NOT block runner invocation. Fall back to no-recall prompt and log a warning.

Deliverables:

A. cli/internal/agentwatch package (new)
   Public surface:
     type Options struct {
        StateDB         *sql.DB
        Agent           string         // required, e.g. "claude-code"
        Runner          Runner         // required for non-dry-run; injectable for tests
        ContextRecaller ContextRecaller // optional; nil => no recall
        Once            bool           // single drain, then return
        Interval        time.Duration  // poll interval when not Once (default 5s)
        Timeout         time.Duration  // per-event runner timeout (default 5m)
        DryRun          bool           // render prompt only; do not call Runner; do not change status
        Logger          *log.Logger    // optional
        MaxRetries      int            // before auto-skip on repeated failures (default 5)
        MaxBatch        int            // events per tick (default 10)
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
     type Stats struct { Picked, Acked, Done, Failed, Skipped int }

     func RunOnce(ctx context.Context, opts Options) (Stats, error)
     func RunLoop(ctx context.Context, opts Options) error  // poll Interval; cancel on ctx; respects flock per-agent

   Pipeline (single tick):
     1. Acquire ~/.aitask/runtime/agent-watch/<agent>.lock via flock LOCK_EX|LOCK_NB. On contention return ErrAlreadyRunning. Skip lock for tmp paths (mirror worker.skipLock heuristic).
     2. Fetch up to opts.MaxBatch (default 10) inbox rows via inbox.ListAgentInbox(opts.Agent, ListOpts{Status:"unread,seen,acked", Limit:max}).
        Filter to the rows currently 'unread' or 'seen' for processing; rows already 'acked' but stale (older than recovery threshold, default 10*Timeout) get treated as orphaned and re-picked; otherwise skip.
     3. For each row to process:
          a. Try inbox.Ack(eventID, agent). If ErrNotApplicable (someone else moved it) -> continue.
          b. If opts.ContextRecaller != nil: best-effort Recall(); on error log warning and proceed.
          c. Render prompt via PromptRenderer (see B).
          d. If opts.DryRun: print prompt to logger or callback; do NOT call runner; do NOT change status from 'acked'. Return early for that event so user can inspect.
          e. Else: ctxRun, cancel := context.WithTimeout(ctx, opts.Timeout); call opts.Runner.Run(ctxRun, prompt); cancel.
          f. On success (err==nil && ExitCode==0): inbox.Done(...).
          g. On non-zero exit or err: inbox.Fail(..., extra=stderrTail/error.Error()). If retry_count >= MaxRetries -> inbox.Skip(..., reason="exceeded MaxRetries").
   No goroutines outside RunLoop.

B. cli/internal/agentwatch/prompt.go
   func RenderPrompt(eventID, agent, recall string, evt PromptEvent) string
   PromptEvent fields: ID, Kind, From, Project, ThreadID, CreatedAt, Body.
   Output mirrors 改造说明.md §12 template (the "You are <agent> in an AITask multi-agent collaboration system" block).
   Function MUST be pure (no DB calls) so it can be exposed to `aitask render-prompt` CLI as a thin wrapper that loads PromptEvent from state.db.

C. CLI wiring (cli/internal/cli)
   New file command_watch.go:
     aitask watch --agent <name> [--once] [--exec <script>|--wake claude|codex|gemini] [--dry-run]
                  [--interval 5s] [--timeout 5m] [--max-retries 5] [--quiet]
     Notes:
       - --agent default = current profile (resolveProfile + DefaultProfileName fallback, mirror command_inbox.go effectiveInboxAgent).
       - Exactly one of --exec / --wake / --dry-run must resolve. If none: default --dry-run when stdout is a TTY; else error.
       - --exec runs the script with prompt on stdin and reads stdout as result; stderr is captured for failure path.
       - --wake values map to one-shot runners:
            claude => exec("claude", "-p", "<prompt-as-arg>")  (claude -p is headless; pass prompt as final arg)
            codex  => exec("codex", "exec") with prompt on stdin
            gemini => exec("gemini") with prompt on stdin
         Implement as a small factory; probe binary on PATH; if missing, return clear error BEFORE lock acquisition.
       - --dry-run prints the prompt to stdout via Printer and exits 0. Does NOT call inbox.Done.

   New file command_render_prompt.go:
     aitask render-prompt --event <id> [--agent <name>] [--no-recall]
       Uses PromptRenderer; recall via backend memory search reusing existing /api/projects/.../memory/search (only if --no-recall not set AND project bound).
       Outputs the rendered prompt as plain text via Printer's prompt branch (so --format json wraps it sensibly).

   Wire newWatchCommand and newRenderPromptCommand into app.go AddCommand alongside the rest.

D. ContextRecaller backend adapter (cli/internal/cli/context_recaller.go)
   - type backendRecaller struct { client *Client; projectID string }
   - Implements agentwatch.ContextRecaller by calling client.GetREST("/api/projects/<projectID>/memory/search", {q: "evt:"+eventID, budget: "2048"}).
   - On 4xx/5xx or empty result: return ("", nil) (graceful degrade).
   - Pure adapter — no SQL, no cobra. Used by both watch and render-prompt commands.

E. Tests
   - cli/internal/agentwatch/agentwatch_test.go:
       * TestRunOnceHappyPath: seed events.ndjson + Ingest via inbox; provide stub Runner returning ExitCode=0; assert inbox row goes to handled.
       * TestRunOnceRunnerFailureMarksFailed.
       * TestRunOnceMaxRetriesSkips.
       * TestSelfMentionNotPicked (preserves existing behavior — events from agent itself are excluded by inbox.ListAgentInbox).
       * TestDryRunDoesNotChangeStatus.
       * TestRecallFailureFallsBackToNoRecall.
   - cli/internal/agentwatch/prompt_test.go: golden-style assertions for the prompt template.
   - cli/internal/cli/command_watch_test.go: smoke a --once --dry-run watcher with HOME=t.TempDir; assert exit 0 + non-empty stdout.
   - cli/internal/cli/command_render_prompt_test.go: --no-recall path returns the rendered prompt.

F. Things you must NOT do
   - Do NOT add stdin injection into running REPL.
   - Do NOT add `aitask watch --wake` modes that require multi-step interactive sessions.
   - Do NOT modify command_inbox.go / command_context.go / command_memory.go / command_worker.go.
   - Do NOT extend state.db schema in this phase.
   - Do NOT add a new top-level command other than `watch` and `render-prompt`.

Return: a single unified diff (against current main) plus a one-paragraph cover summary. No file writes.

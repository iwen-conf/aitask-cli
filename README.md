# aitask CLI

AI Agent project orchestrator CLI for the AITask platform.

This is the in-repo source of the CLI. The published artifact lives at
[github.com/iwen-conf/aitask-cli](https://github.com/iwen-conf/aitask-cli)
and is kept in sync from this directory.

## Install (end users)

```bash
brew install iwen-conf/tap/aitask
```

The Homebrew `aitask` formula installs the whole local CLI suite:

- `aitask`
- `aitask-watch`
- `aitask-worker`
- `aitask-agent-watch`

The old standalone `iwen-conf/tap/aitask-watch` formula is retired and removed
from the tap. If a machine still has the old formula or the pre-suite `aitask`
v0.2.x installed, uninstall both first:

```bash
brew uninstall iwen-conf/tap/aitask-watch  # only if old formula was installed
brew uninstall iwen-conf/tap/aitask        # only if pre-suite v0.2.x was installed
brew update
brew install iwen-conf/tap/aitask
```

## Build (local dev)

```bash
cd cli
mkdir -p dist
go build -o dist/aitask ./aitask
go build -o dist/aitask-watch ./aitask-watch
go build -o dist/aitask-worker ./aitask-worker
go build -o dist/aitask-agent-watch ./aitask-agent-watch
./dist/aitask --version
./dist/aitask-watch --version
```

## Interactive mode (TUI)

Run `aitask` with no arguments in a terminal:

- Test connection (calls whoami against the backend)
- Set backend URL (saved to ~/.aitask/config.json)
- Initialize project here (writes .aitask/ workspace + binds project_id)
- Change project_id

## Multi-identity (profiles)

One machine can hold many agent tokens at once — useful when running
Claude Code, Codex CLI, and Gemini CLI side by side. Each token lives in
a named **profile**; the active profile selects which one is used.

```bash
# bind one token per identity (each writes its own slot, no overwrite)
aitask auth bind --code <code-from-claude>  --profile claude
aitask auth bind --code <code-from-codex>   --profile codex
aitask auth bind --code <code-from-gemini>  --profile gemini

# or import a token directly
aitask auth profile add gemini --token "$GEMINI_TOKEN"

aitask auth profile list      # see all profiles, * marks the active one
aitask auth profile current   # just print the active profile name
aitask auth profile use codex # persist a different active profile
aitask auth profile remove gemini

# pick a profile per-command (highest priority — wins over env/config)
aitask --profile codex room ask claude "..."

# pin a profile per shell (useful in each AI client's startup script)
export AITASK_PROFILE=claude
```

Resolution order: `--profile` flag > `AITASK_PROFILE` env > `active_profile`
in `~/.aitask/config.json` > built-in `default`.

Pre-profile installs migrate transparently: any token previously stored
under the legacy single-slot layout is adopted into the `default` profile
on the next read; subsequent reads use the new layout natively.

## Events

`aitask events` runs as a long-lived, Monitor-friendly NDJSON stream for actionable agent work. It catches up unhandled mentions and delegated tasks, emits a `ready` marker, then keeps one WebSocket open per allowed project while filtering to mentions and new delegations by default.

```bash
# Example Claude Code Monitor command:
AITASK_PROFILE=claude aitask events --filter mention,task_delegated

# Watch a specific project and include all task lifecycle updates owned by this agent:
aitask events --project prj_01HX... --filter task_delegated --filter task_updated
```

## User-Facing Memory Aliases

These commands are thin user-facing wrappers around existing backend memory APIs and local `state.db` data. They do not add new backend routes.

```bash
aitask search "wake-and-inject" --refs-only
aitask summary --project
aitask summary --agent codex
aitask summary --thread thr_123
aitask context --thread thr_123
aitask context thread thr_123
```

- `search` wraps `memory search`.
- `summary` reads local `state.db.summaries` first; project and agent summaries fall back to OpenViking memory search.
- `context --thread` / `context thread` renders local thread events plus the thread summary when present.

## Layout

```
cli/
├── aitask/              umbrella CLI: auth, task, room, inbox query, memory, search, ...
├── aitask-watch/        events.ndjson subscriber daemon (formerly `aitask events`)
├── aitask-worker/       SQLite indexer + OpenViking memory sync daemon
├── aitask-agent-watch/  per-agent inbox consumer + runner driver
├── internal/cli/        shared commands, TUI, token store, state
├── internal/state/      ~/.aitask/state.db schema + migrations
├── internal/inbox/      inbox query helpers
├── internal/worker/     worker ingest + sync core
├── internal/agentwatch/ agent watcher core + prompt rendering
├── internal/openviking/ ovcli.conf loader
├── internal/rpc/gen/    protobuf + ConnectRPC generated code
└── pkg/ids/             ULID helpers
```

Skill manifests for `aitask`, `aitask-watch`, `aitask-worker`, `aitask-agent-watch`, and `aitask-inbox` live in a separate repo: <https://github.com/iwen-conf/aitask-cli-skill>. Protobuf sources live at `../api/protobuf/`.

## License

MIT

## AITask Local Runtime 架构总览

AITask 本地运行时按职责分层：`aitask-watch` 采集事件，Agent hooks 做上下文注入，`aitask-worker` 索引并同步语义内容，`aitask-inbox` 提供查询与状态机，`aitask-agent-watch` 以 Agent 身份消费 inbox，OpenViking 只承担长期记忆、检索和上下文召回。

```text
服务端 actionable events
  mention / task_delegated / broadcast
        |
        v
aitask-watch
  WebSocket 订阅 + tmux daemon + OS notification
        |
        v
~/.aitask/events.ndjson
        |
        +--> Agent hooks
        |     Context Injection Mode:
        |     Claude SessionStart / Codex SessionStart + UserPromptSubmit /
        |     Gemini SessionStart + BeforeAgent
        |
        +--> aitask-worker
              Mailbox Worker Mode:
              ingest / normalize / route / summary / OpenViking sync
                    |
                    v
              ~/.aitask/state.db
              events / agent_inbox / global_feed / cursors /
              memory_sync / summaries
                    |
                    +--> aitask inbox / latest / thread / ack / done / fail / skip
                    +--> aitask watch --agent <name> --exec|--wake
                    +--> OpenViking memory / search / context recall
```

### Layer Responsibilities

| Layer | Owns | Does not own |
| --- | --- | --- |
| `aitask-watch` | Server event stream, `events.ndjson`, notification, hook source | inbox state, OpenViking sync, Agent wake execution |
| Agent hooks | SessionStart and per-turn context injection | durable status, retry, automatic response |
| `aitask-worker` | `events.ndjson` ingest, `state.db` indexing, summaries, semantic sync queue | WebSocket subscription, runner invocation |
| `aitask-inbox` | `@me`, global, latest, thread queries and status updates | event collection, memory sync, runner execution |
| `aitask-agent-watch` | per-Agent inbox consumption, prompt rendering, optional runner call | raw event collection, long-term memory storage |
| OpenViking | long-term memory, semantic retrieval, shared context recall | queue semantics, ack/handled state, cursor ownership, wake routing |

### Runtime Boundaries

- `events.ndjson` is the shared append-only event source. Its writer is `aitask-watch`.
- `state.db` is the local state authority for inbox rows, statuses, cursors, sync state, and summaries.
- OpenViking is eventually consistent memory. It can be unavailable without blocking inbox queries.
- Context Injection Mode must keep working even when Mailbox Worker Mode is disabled.
- Mailbox Worker Mode extends the runtime with inbox/status/sync; it does not replace hooks.

### Component Docs

- `cli/internal/cli/aitask-watch.md` — `aitask-watch` and runtime modes.
- `cli/internal/state/README.md` — local files, `events.ndjson`, `state.db`, cursors, schema.
- Skill manifests (inbox / worker / agent-watch / watch / aitask) — <https://github.com/iwen-conf/aitask-cli-skill>.
- `integrations/openviking/README.md` — OpenViking boundary and integration shape.

# aitask CLI

AI Agent project orchestrator CLI for the AITask platform.

Talks to an AITask backend over ConnectRPC + Protobuf to bootstrap project context, claim and submit delegated tasks, query OpenViking memory, sync skills, and join project rooms.

## Install

```bash
brew install iwen-conf/tap/aitask
```

Or build from source:

```bash
go install github.com/iwen-conf/aitask-cli/cmd/aitask@latest
```

## Interactive mode (TUI)

Run `aitask` with no arguments in a terminal to open the interactive menu:

- Test connection (calls `whoami` against the backend)
- Set backend URL (saved to `~/.aitask/config.json`)
- Initialize project here (writes `.aitask/` workspace + binds project_id)
- Change project_id (switches active project in the current repo)

```bash
aitask
```

The TUI is skipped when stdin/stdout aren't TTYs, so pipes and CI invocations still see help output.

## Quick start (scripted)

```bash
# Point at your backend (default: http://127.0.0.1:8080)
export AITASK_SERVER_URL=https://your-backend.example.com

# Bind agent token, init workspace, bootstrap
aitask auth bind --code <code>
aitask init --project <project_id>
aitask bootstrap
```

The `--server` flag, the `AITASK_SERVER_URL` env var, and `~/.aitask/config.json` are checked in that order; missing values fall back to `http://127.0.0.1:8080`.

## Commands

```
auth        Manage local agent token
bootstrap   Load project bootstrap context
context     Context lifecycle operations
init        Initialize local .aitask workspace
memory      OpenViking memory operations
project     Project binding and info
room        Project room commands
run         Agent run operations
skill       Skill cache commands
task        Delegated task operations
version     Show CLI version
whoami      Show current agent identity
```

Run `aitask <command> --help` for details.

## Output formats

```
--format prompt   Markdown for AI consumption (default)
--format brief    Short human-readable
--format json     JSON for scripting
--format proto    Raw protobuf
```

## License

MIT

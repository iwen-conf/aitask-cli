# Skill: aitask-agent-watch

## 定位

`aitask-agent-watch` 是 AITask 中以"某个 Agent 的身份"消费本地 inbox 的守护进程/单次任务执行器。它读取 `~/.aitask/state.db` 中目标 Agent 名下未处理的事件，渲染 prompt，可选调用 runner（claude / codex / gemini / 自定义 `--exec`），并把执行结果写回 `state.db`（done / failed / skipped）。

## 负责什么

- 以 `--agent <name>` 拉取 `agent_inbox WHERE agent=<name> AND status='unread'`。
- 跳过自己发出的事件（避免回声）。
- 加锁，避免同一 event 被并行处理（`runtime/agent-watch/<agent>.lock` + `agent_inbox` 的 `seen_at` claim）。
- 通过 core 后端拉相关上下文（`/api/projects/.../context/event`、`thread`），调用 OpenViking 召回。
- 调 `cli/internal/cli/command_render_prompt.go` 同款渲染逻辑组装最终 prompt。
- 调 runner：
  - `--exec ./handlers/x.sh`：把 prompt 通过 stdin 喂给外部脚本。
  - `--wake claude|codex|gemini`：启动对应 CLI 的一次性执行（`claude -p`、`codex exec`、`gemini cli`）。
  - `--dry-run`：只渲染 prompt 不调 runner。
- 捕获 stdout / stderr / exit code，把 stdout 当做"任务结果"写一条 `task_done` 事件入 `events.ndjson` 和 `memory_sync`。
- 状态机：`unread → seen → acked → handled` / `failed` / `skipped`，并维护 `retry_count`、`last_error`。

## 不负责什么

- 接 WebSocket 拿原始事件——交给 `aitask-watch`。
- NDJSON → state.db 的索引——交给 `aitask-worker`。
- OpenViking 长期写入——交给 `aitask-worker` + core 后端。
- 决定哪个 runner 该处理什么 event 的"业务逻辑"——本进程只按 `--agent` / `--exec` / `--wake` 选项执行；高级路由由人/上游脚本配置。
- 直接接管正在运行的 REPL / 注入 stdin——第一版只跑一次性 runner（避免破坏 Agent 当前会话）。

## 输入

| 来源 | 内容 |
| --- | --- |
| `~/.aitask/state.db` | `agent_inbox`、`events`、`cursors` |
| Core 后端 | `/api/projects/.../context/event`、`/thread`，OpenViking 召回结果 |
| 命令行参数 | `--agent`、`--once`、`--exec`、`--wake`、`--dry-run`、`--interval`、`--timeout`、`--max-retries`、`--quiet` |
| 环境变量 | `AITASK_PROFILE` 决定默认 `--agent` |
| Runner 进程 stdin | prompt（由本进程注入） |

## 输出

- 写 `~/.aitask/state.db.agent_inbox`（status / retry_count / last_error / handled_at / failed_at）。
- 写 `~/.aitask/events.ndjson` 一条 `task_done` 事件（runner 成功时）。
- 写 `state.db.memory_sync(event_id, status='pending')` 让 `aitask-worker` 把结果同步到 OpenViking。
- stdout：每条 event 的处理结果摘要（`event_id`、`status`、`runner_exit`）。
- stderr：runner 失败原因、prompt 渲染失败原因。

## 核心命令

```bash
aitask-agent-watch --agent claude-code --once --dry-run
aitask-agent-watch --agent claude-code --once --exec ./handlers/claude.sh
aitask-agent-watch --agent codex --wake codex
aitask-agent-watch --agent gemini --interval 5s --max-retries 5

# 兼容入口
aitask watch --agent claude-code ...
```

## 状态文件

- `~/.aitask/state.db.agent_inbox`：本进程是写入主体（与 `aitask ack/done/fail/skip` 共享）。
- `~/.aitask/runtime/agent-watch/<agent>.lock`：file lock，避免同一 Agent 多实例。
- `~/.aitask/events.ndjson`：写入 `task_done` 结果事件。
- 不直接读 `~/.openviking/ovcli.conf`；上下文召回通过 core 后端。

## 与其他组件的关系

- 上游：`aitask-worker` 把事件路由进 `agent_inbox`。
- 下游：runner（claude / codex / gemini / 自定义脚本）的 stdin。
- 调用 core 后端做 context recall + memory write 代理。
- 写 `task_done` 后，`aitask-worker` 下一轮会把它同步到 OpenViking。

## 常见流程

### 1. 单次干活

```text
1. acquire runtime/agent-watch/<agent>.lock
2. SELECT * FROM agent_inbox WHERE agent=? AND status IN ('unread','seen') ORDER BY created_at LIMIT N
3. for each row:
   a. UPDATE status='seen', seen_at=now
   b. fetch context: GET /api/projects/.../context/event?id=<event_id>
   c. render prompt via command_render_prompt 同款逻辑
   d. if --dry-run: print prompt, exit
   e. invoke runner (exec/wake) with prompt on stdin, capture stdout/stderr/exit
   f. on success: UPDATE status='handled', handled_at=now
                  append events.ndjson { kind:'task_done', body:stdout, ... }
                  INSERT memory_sync(event_id, status='pending')
   g. on failure: UPDATE status='failed', failed_at=now, last_error, retry_count++
4. release lock
```

### 2. 长驻

```bash
aitask-agent-watch --agent claude-code --interval 5s --exec ./handlers/claude.sh
# 每 5 秒走一次单次流程；SIGTERM 优雅退出
```

### 3. 调试

```bash
aitask-agent-watch --agent claude-code --once --dry-run --format prompt
# 只看会渲染出的 prompt 长啥样，不执行
```

## 失败与重试策略

- runner 退出码 ≠ 0：标 `failed`，`retry_count++`，下个 cycle 再选。`retry_count > --max-retries` 后改 `skipped`，需人工 `aitask ack <id> --agent <name>` 复位。
- runner 超时：`--timeout`（默认 5min）触发 SIGKILL，按失败处理。
- prompt 渲染失败（context recall 失败）：标 `failed` 一次，重试时若仍失败 `--max-retries` 次后转 `skipped`。
- 锁文件残留：进程意外退出会留 stale lock；新实例启动时检测 PID 不在则覆盖。
- OpenViking 不可达：不影响本地 `handled` 标记，由 worker 下次重试同步。

## Agent 使用注意事项

- **绝对不要**用本进程注入正在跑的 REPL 的 stdin——`--exec` 调起独立短任务进程才是正确姿势。
- `--wake` 模式要求宿主机上对应 Agent CLI 已安装；建议生产用 `--exec` 写显式 handler 脚本。
- 一个 Agent 名只能跑一份本进程，否则同一 event 可能被处理两次（lock 是软锁）。
- runner 的 stdout 会作为 `task_done.body` 写入 `events.ndjson` 并送 OpenViking，注意控制噪音（不要把整个工具调用日志原样输出）。
- 想测试 prompt 但不真跑：`--once --dry-run --format prompt`。
- 任何"全局广播"事件默认不会进 `agent_inbox`——只会进 `global_feed`，本进程不消费它。

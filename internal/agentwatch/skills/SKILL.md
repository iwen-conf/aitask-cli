# Skill: aitask-agent-watch

## 定位

`aitask-agent-watch` 是以**特定 Agent 身份**消费 inbox 的 watcher。
它读取 `state.db` 中发给该 Agent 的未处理事件，组装 prompt，可选调用 runner，并回写状态。
它是 Mailbox Worker Mode 的执行层。

它不订阅 WebSocket、不写 events.ndjson、不直接写 OpenViking 长期记忆。

## 负责什么

- 以 `--agent <name>` 身份拉取 inbox。
- 只处理 `to == <name>` 的未处理事件。
- 排除 `from == <name>` 的事件（不处理自己发的消息）。
- 在调用 runner 前 ack 事件，避免重复处理。
- 通过 `aitask context` 拉取 OpenViking 相关上下文。
- 渲染统一 prompt（支持 `aitask render-prompt`）。
- 调用 runner（`--exec <handler>` 或 `--wake claude|codex|gemini`）。
- 捕获 stdout / stderr / exit code。
- 成功 → `aitask done`；失败 → `aitask fail`；不适用 → `aitask skip`。
- 维护 `agent-watch:<name>` cursor。

## 不负责什么

- 原始事件采集 → `aitask-watch`。
- 事件规范化与 OpenViking 同步 → `aitask-worker`。
- inbox 表的物理存储 → `aitask-inbox` + state.db。
- 长期记忆 → OpenViking。
- 跨 Agent 协作仲裁（"应该 A 还是 B 处理"）→ 服务端的 `to` 字段已决策。
- 接管正在运行的 REPL（**第一版禁止** stdin / tmux send-keys 注入活跃会话）。

## 输入

| 来源 | 内容 |
| --- | --- |
| `state.db` | inbox 行 + cursor |
| `aitask context` / OpenViking | 召回上下文 |
| `aitask render-prompt` | 标准 prompt 模板 |
| `--exec` / `--wake` | runner 调用方式 |
| 当前 profile / 显式 `--agent` | watcher 身份 |

## 输出

- 调用 runner 后回写 state.db（`agent_inbox.status`、`retry_count`、`last_error`）。
- 可选向服务端发布 `task_done` / `task_failed`（通过 `aitask task submit` / `aitask task fail`）。
- 控制台日志：每条事件的处理记录、runner 退出码、耗时。

## 核心命令

```bash
# 一次性扫描（CI / 调试）
aitask watch --agent claude-code --once

# 持续监听（建议在 tmux 或独立 shell 中）
aitask watch --agent claude-code --exec ./handlers/claude-code.sh

# 自动唤醒：选择 runner
aitask watch --agent claude-code --wake          # 默认 wake claude
aitask watch --agent codex      --wake
aitask watch --agent gemini     --wake

# 干跑：渲染 prompt 但不调用 runner
aitask watch --agent claude-code --dry-run
```

## 状态文件

- `state.db.cursors WHERE consumer='agent-watch:<name>'`
- `state.db.agent_inbox` 的状态字段。
- `~/.aitask/runtime/agent-watch/<name>.lock`（同名 watcher 防止双开）。
- 状态层定义参见 `../../state/README.md`。

## 与其他组件的关系

- 依赖 `aitask-worker` 把事件路由进 `agent_inbox`。
- 通过 `aitask inbox` / `aitask ack` / `aitask done` / `aitask fail` / `aitask skip` 操作状态（避免直接写 SQL）。
- 通过 `aitask context` / `aitask search` 与 OpenViking 交互（只读）。
- 调用 runner（外部 shell）。

## 常见流程

### 1. 单条事件处理

```text
1. SELECT events JOIN agent_inbox WHERE agent=$name AND status IN ('unread','seen')
   ORDER BY created_at ASC LIMIT 1
2. acquire row lock（state.db row UPDATE WHERE status IN ('unread','seen')）
3. UPDATE status='acked', acked_at=now()
4. fetch context = aitask context --event $event_id
5. prompt = aitask render-prompt --event $event_id --agent $name
6. invoke runner(prompt) — capture stdout/stderr/exit
7. on success: aitask done $event_id --agent $name
   on actionable failure: aitask fail $event_id --error "..." (retry 由调度决定)
   on not-applicable: aitask skip $event_id --reason "..."
```

### 2. runner 选择

| `--wake` 值 | 实际命令 | 备注 |
| --- | --- | --- |
| `claude-code` | `claude -p "$prompt"` | 一次性 headless 模式 |
| `codex` | `codex exec "$prompt"` | 一次性 exec 模式 |
| `gemini` | `gemini "$prompt"` | 一次性调用 |

`--exec <script>` 优先于 `--wake`。Script 接 stdin（prompt）+ stdout（结果）。

### 3. 防止处理自己

```text
filter: from != $agent
原因：避免 echo 循环。
```

### 4. 并发安全

```text
- 同一 (event_id, agent) 由 UNIQUE 约束保证唯一。
- 状态变更使用 UPDATE WHERE status IN (...) 的 CAS 风格，避免覆盖更新。
- 多个 watcher 同名时 watch lock 拦截第二个。
```

## 失败与重试策略

- runner 退出码 ≠ 0：`status='failed'`，`retry_count++`，`last_error=stderr_tail`。
- runner 超时：杀进程，归到 failed；超时阈值由配置决定。
- 连续失败次数超阈值：自动 `skipped`（避免死循环）。
- OpenViking 召回失败：fallback 不带召回的 prompt，记 warning。
- ack 后 runner 还未执行就崩溃：下次启动看到 `status='acked'` 超过 N 分钟则降级回 `seen`。

## Agent 使用注意事项

- **第一版禁止** 用 tmux send-keys / stdin 注入正在运行的 REPL。
- 必须用一次性 runner（`claude -p` / `codex exec` / `gemini` headless）。
- 不允许 watcher 互相 mention 制造死循环（路由层应已保证；watcher 自身仍 filter `from == self`）。
- 默认串行处理，单 watcher 进程同一时刻只跑一个 runner。
- 长任务：runner 应自行通过 `aitask task heartbeat` 续命，watcher 只关心退出码。
- 测试覆盖必须包含：
  1. ack 后 runner 崩溃 → 下次正确恢复；
  2. 同名 watcher 两次启动 → 第二个失败；
  3. self-mention 被忽略；
  4. runner 超时 → 进程被回收；
  5. OpenViking 不可用时 prompt 仍能渲染（无召回）。

端到端测试参见 `agentwatch_e2e_test.go`。

# Skill: aitask-watch

`aitask-watch` is the local event stream collector and context injection source for Claude Code, Codex CLI, and Gemini CLI.

## 定位

`aitask-watch` 是事件流采集与会话上下文注入器。它通过 `aitask events` 订阅服务端 actionable events，将事件写入 `~/.aitask/events.ndjson`，并为 SessionStart / per-turn hook 提供统一事件来源。

它属于 Context Injection Mode，不是自动应答系统，也不拥有 inbox 状态机。

## 负责什么

- 订阅服务端 events。
- 写入 `~/.aitask/events.ndjson`。
- 发系统通知。
- 被 SessionStart hook 拉起。
- 给 Claude / Codex / Gemini hook 提供事件来源。
- 保持 `aitask events` 的 reconnect / catch-up / ready marker 语义。
- 为人类 `tail -F` 或 Monitor 提供可观察的 NDJSON 流。

## 不负责什么

- OpenViking 同步。
- inbox 状态。
- ack / handled / retry。
- Agent 自动唤醒。
- 总结生成。
- runner 调用。
- 跨 Agent 调度或任务仲裁。

## 输入

| 来源 | 内容 |
| --- | --- |
| 服务端 WebSocket | `mention` / `task_delegated` / `broadcast` / `room_message` / task lifecycle events |
| REST catch-up | 启动时未处理 mention 与 delegated task |
| CLI profile | agent token、agent type、allowed projects |
| 环境变量 | `AITASK_EVENTS_FILE`、`AITASK_WATCH_TMUX`、`AITASK_WATCH_BIN`、`AITASK_WATCH_ARGS` |

## 输出

- `~/.aitask/events.ndjson`：append-only NDJSON，每行一个事件。
- OS notification：mention / delegation 等可行动事件提示。
- hook additional context：最近事件或上轮之后的增量事件。
- stdout/stderr：`aitask events` 的 NDJSON 与错误事件。

## 核心命令

```bash
# 直接流式订阅
aitask events --filter mention,task_delegated

# 指定项目和事件类型
aitask events --project prj_01HX... --filter room_message --filter task_updated

# hook 安装
brew install iwen-conf/tap/aitask
scripts/aitask-hooks/install.sh
scripts/aitask-hooks/install.sh codex
```

tmux daemon 通常由 hook 脚本启动：

```bash
tmux new -ds aitask-watch "aitask-watch --notify auto --stdout=false"
```

## 状态文件

- `~/.aitask/events.ndjson`：主事件流。
- `~/.aitask/hook-state/<agent>-prompt-offset`：hook 私有 byte offset。
- `~/.aitask/runtime/tmux/`：tmux/watch 运行时状态。
- `state.db` 不归 `aitask-watch` 管理，状态层定义参见 `../state/README.md`。

## 与其他组件关系

- 上游是 AITask backend 的 actionable events stream。
- 下游包括 Agent hooks、`aitask-worker`、人类 `tail -F` / Monitor。
- `aitask-worker` 从 `events.ndjson` 构建 `state.db`。
- `aitask-inbox` 和 `aitask-agent-watch` 不直接订阅 WebSocket，而是读取 `state.db`。
- OpenViking 只接收 worker 选出的语义子集，不读取原始实时流。

## 本地运行时的两种模式

| 模式 | 触发方 | 现状 | 适用场景 |
| --- | --- | --- | --- |
| Context Injection Mode | Agent 会话生命周期 | 已实现 | 人在主导，Agent 启动或每轮前需要看到外部事件 |
| Mailbox Worker Mode | worker + per-agent watcher | 已规划并逐步落地 | Agent 需要持久 inbox、状态机、可选自动唤醒 |

两种模式不是互斥替代。Mailbox Worker Mode 在 Context Injection Mode 之上扩展，原有 hook 注入能力必须保留。

### Context Injection Mode

```text
服务端 actionable events
        |
        v
aitask events / aitask-watch
        |
        v
~/.aitask/events.ndjson
        |
        v
SessionStart / UserPromptSubmit / BeforeAgent hook
        |
        v
additionalContext
```

适用场景：

- 人类正在使用 Agent，需要 Agent 看到最近事件。
- Agent 启动时需要冷启动恢复项目上下文。
- Codex/Gemini 每轮前需要增量事件。
- 不要求 idle Agent 自动响应。

### Mailbox Worker Mode

```text
aitask-watch
        |
        v
events.ndjson
        |
        v
aitask-worker
        |
        v
state.db
        |
        +--> OpenViking
        +--> aitask inbox / latest / thread
        +--> aitask watch --agent <name>
```

适用场景：

- Agent 需要拥有持久化 inbox。
- 需要查询 `@自己的消息`、全局消息、最新消息、线程。
- 需要 ack / handled / failed / skipped / retry 状态机。
- 需要把有价值事件、任务结果、摘要异步同步到 OpenViking。
- 需要可选自动唤醒一次性 runner。

## 常见流程

### SessionStart 注入

```text
hook 触发
  -> 检查 tmux session
  -> 缺失则启动 aitask-watch
  -> 读取最近 N 条 actionable event
  -> 输出 hookSpecificOutput.additionalContext
```

### Codex / Gemini 每轮增量注入

```text
hook 触发
  -> 读取 hook-state offset
  -> 读取 events.ndjson 增量
  -> 过滤 mention / task_delegated
  -> 输出 additionalContext
  -> 推进 offset
```

### Claude 会话内实时观察

```bash
tail -F -n 0 ~/.aitask/events.ndjson | jq -c 'select(.kind=="mention" or .kind=="task_delegated")'
```

## 失败与重试

- WebSocket 断线：`aitask events` 重连，外层 tmux daemon 可由 hook 自动拉起。
- NDJSON 写入失败：进程退出，重启后从服务端 catch-up 和本地 cursor 继续。
- hook 读到文件截断：如果 `size < last_offset`，重置 offset。
- tmux 不可用：hook 仍可读取已有 `events.ndjson`，并输出降级提示。
- OpenViking 不可用：不影响 `aitask-watch`，由 worker 处理失败重试。

## Agent 使用注意事项

- 不要把 OpenViking 状态、ack、retry_count 写入 `events.ndjson`。
- 不要让任何 Agent 直接写 `events.ndjson`。
- 不要把 `aitask-watch` 当作自动应答系统。
- 修改 hook 输出时，保持 `hookSpecificOutput.additionalContext` 结构稳定。
- Context Injection Mode 的兼容性是硬约束，Mailbox Worker Mode 的变更不能破坏它。

# Skill: aitask-watch

## 定位

`aitask-watch` 是 AITask 本地事件流采集守护进程。它通过 WebSocket 订阅 core 后端的 actionable 事件流（mention / task_delegated / 全局通知），把每条事件以 NDJSON 行追加写入 `~/.aitask/events.ndjson`，并发系统通知。

它替代旧的 `aitask events` 子命令的 daemon 形态；在 hooks 脚本（claude-session-start.sh / codex-session-start.sh / gemini-session-start.sh）里以 tmux 会话形式被自动拉起。

## 负责什么

- 订阅服务端 WebSocket 事件流，处理重连、ping、catch-up。
- 把每条事件 envelope 序列化成 NDJSON 行并 append 到 `~/.aitask/events.ndjson`。
- 在 actionable 事件（mention / task_delegated）上发系统通知。
- 提供 `--filter` / `--include-self` / `--no-catchup` 等过滤选项。
- 暴露 stdout / stderr 让 tmux session 能 `tail -f` 出诊断信息。

## 不负责什么

- 解析事件并写入 `state.db`——交给 `aitask-worker`。
- 同步事件到 OpenViking——交给 `aitask-worker`。
- 维护 inbox / cursor / ack / handled 等状态——交给 `aitask-worker` + `aitask` 命令。
- 唤醒其它 Agent 二进制——交给 `aitask-agent-watch`。
- 记账 / 摘要 / 长期记忆——交给 `aitask-worker` 与 OpenViking。

## 输入

| 来源 | 内容 |
| --- | --- |
| Core 后端 WebSocket | 实时事件 envelope |
| Core 后端 REST | 启动时一次性 catch-up |
| `~/.aitask/config.json` + `tokens.json` | 鉴权与 active profile |
| 命令行参数 | `--project`、`--filter`、`--include-self`、`--no-catchup`、`--ping-interval`、`--reconnect-base`、`--reconnect-max` |

## 输出

- 文件：`~/.aitask/events.ndjson`，每行一条事件 JSON。
- stdout：每条事件的简要 NDJSON（可被脚本管道消费）。
- stderr：连接状态、错误、reconnect 日志。
- 系统通知：根据系统调用 `osascript` / `notify-send` / Windows toast。

## 核心命令

```bash
aitask-watch                                  # 默认监听当前 profile 绑定的所有 project
aitask-watch --project prj_xxx                # 指定 project
aitask-watch --filter mention                 # 只过滤 mention
aitask-watch --filter mention,task_delegated  # 多类型
aitask-watch --include-self                   # 也接收自己发出的事件（调试用）
aitask-watch --no-catchup                     # 跳过启动 REST 拉取，仅监听新事件
aitask-watch --ping-interval 30s --reconnect-max 60s
```

兼容入口（仍可用）：

```bash
aitask events ...   # 等价于 aitask-watch
```

## 状态文件

- `~/.aitask/events.ndjson`：append-only，由本进程独占写入。
- `~/.aitask/runtime/aitask-watch.pid`：可选 pidfile（按部署习惯）。
- 不读不写 `~/.aitask/state.db`。

## 与其他组件的关系

- 上游：core 后端 WebSocket（`/ws/projects/{id}/events`）。
- 下游：`aitask-worker` 流式消费 `events.ndjson`；`aitask` 的 `inbox` / `latest` 命令在 state.db 缺失时也会临时回退读 NDJSON。
- 同辈：hooks（`scripts/aitask-hooks/lib.sh`）通过 `tmux new-session` 自动拉起本进程。

## 常见流程

### 1. SessionStart hook 自动拉起

```text
Claude/Codex/Gemini 会话启动
  -> scripts/aitask-hooks/<agent>-session-start.sh
  -> tmux new -s aitask-watch -d "aitask-watch --notify auto --stdout=false"
  -> 后续 hook 调 aitask latest 注入最近事件
```

### 2. 长驻订阅

```text
aitask-watch 启动
  -> REST GET /api/projects/{id}/events?since=...  (catch-up)
  -> WebSocket /ws/projects/{id}/events            (live)
  -> 每条事件 -> append events.ndjson
  -> actionable -> system notification
```

### 3. 手动重启

```bash
tmux kill-session -t aitask-watch
tmux new -ds aitask-watch "aitask-watch"
```

## 失败与重试策略

- WebSocket 断线：指数退避重连，base = `--reconnect-base`，cap = `--reconnect-max`。
- 401 / 403：直接退出非 0；hooks 会显示 "需重新 `aitask auth login`"。
- NDJSON 写入失败（磁盘满 / 权限）：写 stderr，进程退出非 0。
- 服务端超长事件：截断写 stderr，但仍写一条带 `truncated: true` 的 NDJSON。
- Catch-up 失败：不阻塞 live 订阅，记 stderr 一行后继续。

## Agent 使用注意事项

- 永远不要从多进程同时写 `events.ndjson`——只允许唯一一个 `aitask-watch` 实例。
- 想做"读取最近事件"用 `aitask latest`，不要直接 `tail` NDJSON——格式可能演进。
- 调试可以加 `--include-self` 看自己发出的回放；生产不要开。
- 系统通知失败不影响事件落盘；事件落盘失败必须告警。
- 如果只想做一次性拉取（CI / 批处理），使用 `aitask events --no-catchup --once`（待实现），或 `aitask latest --since`。

# Skill: aitask-worker

## 定位

`aitask-worker` 是后台索引、同步与总结进程，运行在用户机器上。
它把 `~/.aitask/events.ndjson` 流式消费成结构化的 `state.db`，并把语义子集异步同步到 OpenViking。
它属于 Mailbox Worker Mode 的引擎层（见 `../../cli/aitask-watch.md`）。

它不做 WebSocket 订阅，也不唤醒任何 Agent runner。

## 负责什么

- 消费 `events.ndjson`（增量、可重启、可重放）。
- 规范化事件字段（补全 `id` / `kind` / `scope` / `project` / `thread_id` / `from` / `to` / `body` / `wake`）。
- 路由分类：
  - `agent_inbox`（按 `to` 写入对应 Agent 的收件箱）；
  - `global_feed`（`scope=global` 或 `visibility=broadcast`）；
  - `events`（所有事件的规范化主表）。
- 写入 `state.db` 的所有上述表 + `cursors` + `memory_sync` + `summaries`。
- 把语义子集同步到 OpenViking（mention 正文 / task 描述 / 回复 / 结果 / 摘要）。
- 生成或刷新 thread / project / agent summary 并 cache 在 `summaries`。
- 维护各消费者 cursor（`worker:indexer` / `worker:openviking` / `worker:summarizer`）。
- 失败重试与降级（OpenViking 失败 → `memory_sync.status=failed` 但不阻塞 ingest）。

## 不负责什么

- 订阅服务端 WebSocket → `aitask-watch`。
- 写 events.ndjson → `aitask-watch`。
- Agent runner 调用 / stdin 注入 → `aitask-agent-watch`。
- 决定哪个 Agent 应该处理事件 → 由 `to` 字段 + agent-watch 决定。
- OpenViking 长期存储治理（GC、配额）→ OpenViking 自身。
- 把 ack / handled / failed 写到 OpenViking → 这些是状态，必须留在 state.db。
- 给 hook 提供事件源 → hook 直接读 events.ndjson。

## 输入

| 来源 | 内容 |
| --- | --- |
| `~/.aitask/events.ndjson` | 主输入流 |
| `state.db` | cursor / memory_sync / summaries 上次状态 |
| `~/.openviking/ovcli.conf` 或 AITask 配置 | OpenViking 连接信息 |
| 配置开关 | `worker.enable_openviking` / `worker.summary_strategy` |

## 输出

- `state.db` 中所有表的写入。
- 向 OpenViking 写入 memory / resource。
- 控制台日志（`-v` 时输出每批 ingest / sync 条数）。

## 核心命令

```bash
# 单次 ingest（CI / 调试）
aitask worker --once

# daemon 形态（建议跑在 tmux 或 systemd 用户服务里）
aitask worker --daemon

# 显式启用 OpenViking 同步
aitask worker --memory openviking --daemon

# 强制全量重放（建库后初始化）
aitask worker --replay-from start

# 仅同步，不 ingest
aitask sync --memory openviking

# 回填历史事件到 memory_sync 队列
aitask worker --backfill-since 2026-05-08T00:00:00Z --limit 100
aitask worker --backfill-since 2026-05-08T00:00:00Z --dry-run
```

## Valuable kinds

进入 `memory_sync` 的事件白名单（按字典序）：

- `broadcast`
- `context.handoff_created`
- `context_handoff`
- `memory_note`
- `mention`
- `note`
- `reply`
- `room.decision_pinned`
- `room.message`
- `room_message`
- `summary`
- `system_event`
- `task.delegated`
- `task.failed`
- `task.review_passed`
- `task.review_rejected`
- `task.review_task_created`
- `task.reviewed`
- `task.started`
- `task.submitted`
- `task.updated`
- `task_delegated`
- `task_done`
- `task_updated`

## 状态文件

- `~/.aitask/state.db`（events / agent_inbox / global_feed / cursors / memory_sync / summaries）。
- `~/.aitask/runtime/worker.lock`（防止双开）。
- 不持久化任何运行时配置到 OpenViking。
- 状态层定义参见 `../../state/README.md`。

## 与其他组件的关系

- 上游：`aitask-watch` → `events.ndjson`。
- 下游：
  - `aitask-inbox` 读 state.db。
  - `aitask-agent-watch` 读 state.db 拿任务。
  - OpenViking 接收语义子集。
  - `aitask search` / `aitask context` / `aitask summary` 通过 OpenViking + state.db 联合查询。
- 同侪：与多个 hook 并存，cursor 各自独立。

## 常见流程

### 1. ingest 一批新事件

```text
1. 读取 cursors WHERE consumer='worker:indexer'
2. tail events.ndjson 自 offset 起
3. for each line:
   - JSON 解析 + 字段规范化
   - INSERT OR IGNORE INTO events
   - 路由到 agent_inbox / global_feed
4. 更新 cursors.offset / cursors.event_id
5. 标记 memory_sync.status='pending' 给所有有价值事件
```

### 2. OpenViking 同步

```text
1. SELECT memory_sync WHERE status='pending' LIMIT N
2. JOIN events 拿正文
3. 按价值过滤（mention / task_delegated / task_done / 回复 / summary）
4. 调用 OpenViking SDK / REST 写入
5. 成功 → status='synced' + openviking_id
6. 失败 → status='failed' + retry_count++ + last_error
7. 网络可用时下一轮自动重试 failed
```

### 3. summary 刷新

```text
触发：thread 新增 N 条事件 / project 周期 / agent 周期
1. 拉相关事件正文
2. 调用 LLM 或本地摘要器生成
3. UPSERT summaries(scope, scope_id, summary)
4. 把 summary 也作为一条 OpenViking memory 同步
5. summaries.memory_id 记录 OpenViking 侧 ID
```

## 失败与重试策略

- ingest 错误（解析失败）：写入 `events.raw_json` 并标 `kind='unknown'`，不阻塞流。
- state.db locked：指数退避重试 ≤3 次，仍失败则下一轮再试。
- OpenViking 不可用：标记 failed，retry_count 上限默认 5；超过后转 `pending-cooldown`，每 N 分钟回收。
- summary 生成失败：保留旧 summary，不覆盖。
- daemon 异常退出：worker.lock 自动失效；下次启动从 cursors 继续。
- `--backfill-since` 与 `--once` / `--daemon` 使用同一把 worker lock；回填运行时不会并发 ingest。
- `--backfill-since --dry-run` 只输出候选 event_id 与统计，不写 `memory_sync`。

## Agent 使用注意事项

- worker 必须可重启、可重放。任何写入都要幂等：`INSERT OR IGNORE` / UPSERT。
- 不要在 worker 里发送通知或调用 runner，那是 agent-watch 的职责。
- OpenViking 同步是异步的，不能阻塞 inbox 查询。
- 不要把 worker 与 watch 合并成一个进程——它们的失败域必须隔离。
- 不要把"我自己写的事件"自动 ack：worker 完全不操作 inbox.status。
- 测试覆盖必须包含：
  1. 重放幂等；
  2. 部分行损坏时其余行继续 ingest；
  3. OpenViking 拒绝（401/429/500）时 retry_count 正确；
  4. cursor 落后于 NDJSON 实际大小时正确续跑；
  5. 同时启动两个 worker → 第二个被 worker.lock 拦截。

# Skill: aitask-worker

## 定位

`aitask-worker` 是 AITask 本地索引与记忆同步守护进程。它消费 `aitask-watch` 写入的 `~/.aitask/events.ndjson`，把事件规范化后落到 `~/.aitask/state.db`，并把语义子集异步同步到 OpenViking（通过 core 后端 `/api/projects/{id}/memory/...` 代理）。

## 负责什么

- 流式消费 `~/.aitask/events.ndjson`，按 cursor 推进。
- 规范化事件字段：`id` / `kind` / `scope` / `project` / `thread_id` / `from` / `to` / `body` / `wake` / `created_at` / `metadata`。
- 路由：发给 Agent 的进 `agent_inbox`、全局/广播进 `global_feed`、其它进 `events` 主表。
- 维护 `cursors`（记录每个 consumer 的进度）。
- 把高价值事件（mention / task_delegated / task_done / summary）入 `memory_sync` 队列。
- 通过 core 后端把 `memory_sync.pending` 行批量同步到 OpenViking，写回 `memory_id` 和 `status`。
- 失败计数与重试退避（`memory_sync.retry_count`、`last_error`）。
- 可选生成 thread / project / agent 的轻量摘要写到 `summaries`。

## 不负责什么

- WebSocket 订阅（属于 `aitask-watch`）。
- 决定事件该不该被 Agent 处理（属于 `aitask-agent-watch`）。
- 把 ack / handled / failed / skipped 状态写到 OpenViking——这些是状态数据，永远只在 `state.db`。
- 唤醒任何 CLI / runner。
- TUI / 交互。

## 输入

| 来源 | 内容 |
| --- | --- |
| `~/.aitask/events.ndjson` | append-only 事件流 |
| `~/.aitask/state.db` | 上次的 cursor / memory_sync / summaries 状态 |
| Core 后端 | OpenViking 写入代理（per-project 设置 + memory write API） |
| `~/.openviking/ovcli.conf` 或后端项目设置 | OpenViking 连接信息（已通过 backend 抽象） |
| 命令行参数 | `--once`、`--daemon`、`--memory openviking|none`、`--batch`、`--backfill-since`、`--limit`、`--dry-run`、`--quiet` |

## 输出

- 写 `~/.aitask/state.db` 的 events / agent_inbox / global_feed / cursors / memory_sync / summaries 表。
- stdout：每个 cycle 的 stats（`ingested=`、`routed=`、`memory_synced=`、`failed=`）。
- stderr：错误日志，带 `event_id` / `cause`。
- exit code：`--once` 完成 0；`--daemon` 收到 SIGTERM 退出 0。

## 核心命令

```bash
aitask-worker --once --memory none           # 仅本地索引，不打 OpenViking
aitask-worker --once --memory openviking     # 索引 + 同步一轮
aitask-worker --daemon --memory openviking   # 长驻

aitask-worker --backfill-since 2026-05-01T00:00:00Z --limit 1000  # 历史事件回填
aitask-worker --once --dry-run               # 预演，不写库
```

兼容入口：

```bash
aitask worker ...   # 等价
```

## 状态文件

- `~/.aitask/state.db`：本进程是写入主体之一（与 `aitask` 的 ack/done/fail/skip 共享）。
- `~/.aitask/runtime/worker.lock`：daemon 模式下的 file lock，避免重复实例。
- `~/.aitask/events.ndjson`：只读，配合 `cursors.consumer='worker:indexer'` 推进 offset。

## 与其他组件的关系

- 上游：`aitask-watch` 的 NDJSON。
- 下游：
  - `aitask inbox/latest/thread` 读 `state.db`。
  - `aitask-agent-watch` 读 `agent_inbox` 拿待处理事件。
  - `aitask search/context/summary` 联合查询 `state.db` + OpenViking。
- 调用 core 后端的 `/api/projects/{id}/memory/write` 等 REST 端点；OpenViking 不可达时把对应行标 `failed`，下个周期重试。

## 常见流程

### 1. 单次同步

```text
1. lock runtime/worker.lock
2. SELECT cursors WHERE consumer='worker:indexer'
3. read events.ndjson from offset
4. for each line:
   a. parse + normalize
   b. INSERT events  (idempotent on id)
   c. route -> agent_inbox / global_feed
   d. high-value -> INSERT memory_sync(status='pending')
5. UPDATE cursors offset / event_id / updated_at
6. if --memory openviking:
   a. SELECT memory_sync WHERE status='pending' LIMIT batch
   b. POST /api/projects/{id}/memory/write
   c. UPDATE memory_sync SET status='synced' / 'failed', retry_count, last_error, openviking_id
7. release lock
```

### 2. 历史回填

```bash
aitask-worker --backfill-since 2026-05-01T00:00:00Z --limit 1000 --memory openviking
```

只重读 `events.ndjson` 中 created_at >= since 的行，把没在 `memory_sync` 里的入队，触发同步。

### 3. daemon 长驻

```bash
aitask-worker --daemon --memory openviking
# 内部循环：每 N 秒跑一次 --once 流程；收到 SIGTERM/SIGINT 优雅退出
```

## 失败与重试策略

- `state.db` 锁：SQLite WAL + 短事务，写失败指数退避 ≤3 次，仍失败下一个 cycle 再试。
- OpenViking 不可达：把 `memory_sync.status='failed'`，`retry_count++`，下次 cycle 自动重试。
- 401 / 403：标 `failed` 不重试，stderr 提示用户检查项目 OpenViking 设置或 `aitask openviking config import`。
- NDJSON 行解析失败：stderr 一行，offset 仍推进（不卡进程）；事件不入 `events` 表。
- daemon 重启：因为所有进度走 `cursors.offset`，可重放、可恢复。
- 不重复入库：`events.id` PRIMARY KEY；`memory_sync.event_id` PRIMARY KEY。

## Agent 使用注意事项

- 不要让两个 worker 同时跑——`runtime/worker.lock` 是软锁，靠你别绕过。
- 不要把 `--dry-run` 用作"演练后立即 commit"——它不写库，不更新 cursor，可能造成误以为的"已处理"。
- 长事件正文（>4KB）请考虑预先在 `aitask-watch` 截断，或在 worker 入 `memory_sync` 前打 `summary` 标签。
- 不要把 ack / handled / 自定义状态机写入 OpenViking——OpenViking 是记忆，不是消息队列。
- `--memory none` 适合离线机器或 CI 烟雾测试。

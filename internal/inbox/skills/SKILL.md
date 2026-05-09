# Skill: aitask-inbox

## 定位

`aitask-inbox` 是 Agent 邮箱与消息查询子命令族，运行于 CLI 进程内，状态写入本地 `~/.aitask/state.db`。
它是 Mailbox Worker Mode 的查询入口（见 `../../cli/aitask-watch.md`）。

它不订阅事件、不同步 OpenViking、不调用 runner。

## 负责什么

- `@自己的消息`：`aitask inbox --agent <name>`。
- 全局消息：`aitask inbox --global`。
- 最新消息：`aitask latest --limit N`。
- 线程消息：`aitask thread <thread_id>`。
- 状态更新：`aitask ack` / `aitask done` / `aitask fail` / `aitask skip`。
- 维护 `agent_inbox` / `global_feed` / `events` / `cursors` 表。
- per-agent cursor（`agent-watch:<name>` / `hook:<name>`）。
- thread_id / event_id 维度的查询。

## 不负责什么

- 语义检索 → `aitask search`（OpenViking 集成）。
- 长期记忆 → OpenViking。
- WebSocket 订阅 → `aitask-watch`。
- summary 生成 → `aitask-worker`。
- Agent 唤醒 → `aitask-agent-watch`。
- 把"自己发出的消息"展示给自己（默认排除）。

## 输入

| 来源 | 内容 |
| --- | --- |
| `~/.aitask/state.db` | 主要数据源（events / agent_inbox / global_feed） |
| `~/.aitask/events.ndjson` | state.db 缺失时做一次只读 ingest 的回退源 |
| CLI 参数 | `--agent` / `--global` / `--limit` / `--status` / `--since` |
| 当前 profile | 决定默认 `--agent` |

## 输出

- 默认 Prompt 友好的多行文本（与现有 `aitask` 子命令一致）。
- 支持 `--format json` / `--format proto`，与 root command 全局开关一致。
- 状态命令成功时只输出受影响行数 + event_id。

## 核心命令

```bash
# 查询
aitask inbox --agent claude-code                  # 默认仅未处理（unread/seen/acked）
aitask inbox --agent claude-code --status all
aitask inbox --global
aitask latest --limit 20
aitask thread thr_456

# 状态
aitask ack  evt_123 --agent claude-code
aitask done evt_123 --agent claude-code
aitask fail evt_123 --agent claude-code --error "..."
aitask skip evt_123 --agent claude-code --reason "not actionable"
```

## 状态文件

主：`~/.aitask/state.db`（schema 见 `../../state/README.md`）。
回退：`~/.aitask/events.ndjson`。
状态层定义参见 `../../state/README.md`。

## 与其他组件的关系

- 依赖 `aitask-worker` 把 NDJSON 索引到 state.db；worker 缺席时使用一次性 ingest 回退。
- 被 `aitask-agent-watch` 使用：watcher 在 ack 之前先用 inbox 查询拿到任务列表。
- 被人类直接使用做巡检。
- 被 hook 脚本可选用于"还有 N 条未处理"摘要（不强制）。

## 常见流程

### 1. 查询 @claude-code 的未处理消息

```text
1. 解析 --agent → claude-code
2. 打开 state.db；不存在则 ingest events.ndjson 到临时 in-memory store
3. SELECT events JOIN agent_inbox WHERE agent='claude-code' AND status IN ('unread','seen','acked')
4. 按 created_at ASC 输出
```

### 2. 标记 done

```text
1. 解析 event_id + --agent
2. BEGIN IMMEDIATE
3. UPDATE agent_inbox SET status='handled', handled_at=now() WHERE event_id=? AND agent=?
4. 若行数 0：报错（事件不存在或不属于该 Agent）
5. COMMIT
```

### 3. 全局消息

```text
SELECT events JOIN global_feed
  WHERE (project = $current_project OR visibility = 'broadcast')
  ORDER BY created_at DESC
  LIMIT $limit
```

## 失败与重试策略

- state.db 锁竞争：使用 SQLite WAL + 短事务，不做应用层重试。
- ingest 回退（events.ndjson）：纯只读，不写盘；仅服务于查询。
- 不存在的 event_id：返回 `error_codes.md` 中的 `not_found`，不静默成功。
- 不允许跨 Agent 修改：A 不能 ack/done B 的 inbox 行。

## Agent 使用注意事项

- 默认排除当前 profile 自己发出的事件（`from = current_agent` 不出现在 inbox）。
- 排除原则与 `aitask events --include-self=false` 一致。
- 状态命令默认只接受当前 profile，除非 `--agent` 显式指定。
- 不要在 inbox 里直接调用 OpenViking——召回是另一个命令。
- 数据库 schema 由 worker / inbox 共同维护，迁移脚本必须幂等。
- 测试覆盖必须包含：
  1. state.db 不存在时回退 ingest 能查询；
  2. 同一事件不会被同一 Agent ack 两次；
  3. `--agent` 缺省取 active profile；
  4. 排除 `from == agent` 的事件；
  5. 大量事件分页正确。

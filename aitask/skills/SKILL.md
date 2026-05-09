# Skill: aitask CLI

## 定位

`aitask` 是 AITask 本地协作系统的统一入口 CLI。它聚合鉴权、项目、任务、上下文、记忆、Skill、房间、Inbox 查询等所有交互式子命令；不是 daemon。

## 负责什么

- 用户身份与项目绑定：`aitask auth`、`aitask whoami`、`aitask init`、`aitask project`。
- 任务派发与执行：`aitask task`、`aitask run`、`aitask bootstrap`。
- 上下文管理：`aitask context`、`aitask context handoff`、`aitask render-prompt`。
- 检索与记忆：`aitask search`、`aitask memory`、`aitask summary`、`aitask openviking config`。
- 本地 Inbox 查询：`aitask inbox`、`aitask latest`、`aitask thread`、`aitask ack/done/fail/skip`。
- 房间协作：`aitask room`。
- Skill 同步：`aitask skill`。
- 内嵌兼容入口：`aitask events` / `aitask worker` / `aitask watch` 仍可使用，等价于独立的 `aitask-watch` / `aitask-worker` / `aitask-agent-watch` 二进制。

## 不负责什么

- 持续运行的 daemon——长驻进程交给 `aitask-watch`、`aitask-worker`、`aitask-agent-watch`。
- 直接连接 OpenViking——所有 OV 操作走 core 后端 `/api/projects/{id}/openviking/...`。
- WebSocket 事件订阅与 NDJSON 写入——交给 `aitask-watch`。
- SQLite 索引重建——交给 `aitask-worker`。
- 自动唤醒其它 Agent CLI——交给 `aitask-agent-watch`。

## 输入

| 来源 | 内容 |
| --- | --- |
| `~/.aitask/config.json` | active profile / 项目 ID 缓存 |
| `~/.aitask/tokens.json` | 每个 server+profile 的鉴权 token |
| `~/.aitask/events.ndjson` | inbox 命令在 state.db 缺失时回退读源 |
| `~/.aitask/state.db` | inbox / latest / thread / ack-done-fail-skip 主数据源 |
| `~/.openviking/ovcli.conf` | `aitask openviking config import` 读取的 OpenViking CLI 配置 |
| 环境变量 | `AITASK_SERVER`、`AITASK_PROFILE`、`OPENVIKING_CLI_CONFIG_FILE` |
| 命令行参数 | `--server`、`--project`、`--profile`、`--format`、`--timeout` |

## 输出

- stdout：默认 `prompt` 文本；`--format json|brief|proto` 可切换。
- stderr：错误信息（带 `code` / `details` / `retryable` 提示）。
- exit code：成功 0；失败 1；`--help` 0。

## 核心命令

```bash
aitask auth login --server https://...
aitask whoami
aitask init                       # 在仓库根创建 .aitask/project.md
aitask project create / use ...
aitask bootstrap                  # 拉项目上下文
aitask task list / claim / submit / fail
aitask run --plan ...             # 执行单步
aitask context status / report / compact
aitask context handoff prepare / submit / current
aitask render-prompt --event <id> --agent <name>
aitask search "<query>"           # 优先 OpenViking，回退本地 rg
aitask memory search / read / write
aitask summary --project / --thread / --agent
aitask openviking config show / import
aitask inbox --agent <name> | --global
aitask latest --limit 20
aitask thread <thread_id>
aitask ack/done/fail/skip <event_id> --agent <name>
aitask room ...
aitask skill sync / list
```

## 状态文件

- `~/.aitask/config.json`、`~/.aitask/tokens.json`：用户级配置与 token。
- `~/.aitask/state.db`：inbox / latest / thread / 状态命令依赖。
- `~/.aitask/events.ndjson`：state.db 缺失时的只读回退源。
- `~/.aitask/runtime/`：worker / agent-watch lock 文件，本 CLI 读不写。
- `.aitask/project.md`：项目工作区元数据。

## 与其他组件的关系

- 调用 core 后端 REST/Connect-RPC：`/api/projects/...`、`/api/tasks/...`、`/api/context/...`。
- 共享 `~/.aitask/state.db` 与 `aitask-worker`、`aitask-agent-watch`：本 CLI 只读为主，状态命令（ack/done/fail/skip）写。
- `aitask events` / `worker` / `watch` 子命令在内部复用 `aitask-watch` / `aitask-worker` / `aitask-agent-watch` 的实现；后续也可通过独立二进制运行。
- 不直连 OpenViking。所有 OV 写入由 core 后端代理。

## 常见流程

### 1. 全新机器接入项目

```text
aitask auth login --server <url>
aitask init                      # 在项目仓库创建 .aitask/project.md
aitask whoami                    # 验证身份
aitask bootstrap                 # 拉一次项目上下文
```

### 2. 拉一条已分配任务

```text
aitask task list --status delegated
aitask task claim <id>
aitask render-prompt --event <event_id> --agent <self>
... 干活 ...
aitask task submit <id> --from result.md
```

### 3. 把 ovcli.conf 同步到项目设置

```text
aitask openviking config show       # 确认会读哪个文件
aitask openviking config import --dry-run --project <id>
aitask openviking config import --project <id>
```

## 失败与重试策略

- 网络错误：`enhanceCommandError` 自动注入 `hint`，并把 `code` / `retryable` 透传到 stderr。
- 401 / 403：提示用户重新 `aitask auth login`，不静默重试。
- state.db 锁竞争：底层 SQLite WAL + 短事务；CLI 不做应用层重试。
- OpenViking 不可达时，`aitask search` 自动回退 `rg`，`aitask memory write` 直接报错。

## Agent 使用注意事项

- 所有命令默认是无副作用查询；写命令（`task submit`、`memory write`、`ack/done/fail/skip`、`openviking config import`）请显式确认。
- `--format json` 适合 Agent 解析；`prompt` 适合人类。
- `--project` 优先级高于 `~/.aitask/config.json` 中的 active project。
- 每条命令的退出码与 `code` 字段已对齐 `cli/internal/cli/error_codes.md`。
- 关于事件采集 / 索引 / Agent 唤醒，请查阅 `aitask-watch` / `aitask-worker` / `aitask-agent-watch` 的 SKILL.md。

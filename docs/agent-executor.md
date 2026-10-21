# 执行器与工作区设计

> 目标：让代码工作室、文档生成这类场景真正能读文件、改代码、跑测试、产出文档，同时不把宿主机的文件和命令权限直接交给模型。

## 1. 先说结论

- **代码工作室需要执行器**：只靠聊天模型改不了仓库，必须能读写工作区、运行测试、看 diff。
- **但不能直接给宿主 shell**：PI-agent 自己明确说「没有内置权限系统，默认以启动用户的权限运行」，项目信任只限制启动资源，不限制工具执行。我们面向多用户，直接给宿主权限等于把 `$HOME`、密钥、其他用户数据一起交出去。
- **默认用 Docker 沙箱**：只挂载一个 per-conversation 的 workspace；非 root、无 docker socket、无宿主网络、只读根文件系统、CPU/内存/PID/时间限制。
- **执行器与会话分离**：角色聊天继续走现有 `Orchestrator`；代码/文档任务走新的 `Workspace + Run + Executor`，不要把两套循环混在一起。
- **写操作可回滚**：每次 Run 前做 git snapshot，写文件产生 diff/patch，Run 结束可一键 revert。
- **高风险操作要审批**：读自动；workspace 内写自动但留 diff；安装依赖、联网、`git push/reset`、删除大量文件、越界路径必须人工批准。

## 2. PI-agent 给我们的参考

源码已 clone 到 `research/pi`（临时研究目录，不进产品）。

值得借的：

1. **Agent loop**：模型返回 tool calls → 执行 → 把 tool result 放回上下文 → 再请求模型，直到没有 tool call。事件流覆盖 `tool_execution_start/update/end`、turn、agent end。
2. **Tool 语义**：
   - `read`：按行读取、分页、图片附件、输出截断；
   - `write`：整文件写；
   - `edit`：精确文本替换，多个不重叠 edits 一次提交，生成 diff/patch；
   - `bash`：cwd、超时、取消、流式输出、输出截断、完整输出落盘。
3. **可插拔 operations**：read/edit/bash 都把文件/命令操作抽成 `Operations`，可以替换成 SSH/远程/沙箱实现。
4. **RPC/SDK**：Pi 提供 JSONL RPC 长驻子进程（适合 Go 后端跨语言集成）和 TypeScript SDK。RPC 有 `prompt/steer/followUp/abort/bash` 等命令，事件可流式消费。
5. **Session tree + compaction**：会话是树，分支/恢复/压缩都有明确语义；我们的记忆层已经解决长期记忆，不需要照搬整棵 session tree，但「原始事件保留、上下文可压缩」值得借。
6. **Skills/extensions**：按需加载技能说明和工具，适合以后给代码/文档场景装不同工具包。

不能照搬的：

- Pi 默认没有文件/进程/网络/凭据的权限系统，工具直接用宿主用户权限运行；`security.md` 明确说「不要靠看 transcript、项目信任、review 变化来建立安全边界」。
- Pi 的 project trust 只控制加载哪些项目资源，不限制工具能访问什么。
- Pi 推荐用容器/VM 隔离；这正是我们产品必须默认做的。

## 3. 分层

```text
┌────────────────────────────────────────────────────┐
│ 会话层（已有）                                      │
│ 角色 / 群聊 / 记忆 / 场景 / SSE                     │
│ 角色可以讨论、拆任务、 review，但不直接握有宿主权限 │
└──────────────────────┬─────────────────────────────┘
                       │ 用户确认「执行」
┌──────────────────────▼─────────────────────────────┐
│ 工作层（新增）                                      │
│ Workspace · Run · ToolCall · Approval · Artifact    │
│ AgentRunner：模型 tool-call 循环 + 事件流            │
│ Snapshot / Diff / Revert / Audit                    │
└──────────────────────┬─────────────────────────────┘
                       │ Executor 接口
┌──────────────────────▼─────────────────────────────┐
│ 运行时层（新增）                                    │
│ LocalSandbox（仅开发）                              │
│ DockerSandbox（默认）                               │
│ RemoteSandbox / Pi RPC sidecar（可选）              │
└────────────────────────────────────────────────────┘
```

### 3.1 会话层

现有 `Orchestrator` 保持「角色自主发言」。代码工作室里可以有一个 **operator** 角色负责执行，其他角色做架构/审校/测试建议。operator 的工具调用必须经过工作层，不允许直接 `exec.Command` 宿主命令。

### 3.2 工作层

一个 Run 是「一次任务」：

```text
Run
 ├─ workspace_id
 ├─ conversation_id / user_id
 ├─ status: queued | running | waiting_approval | done | failed | cancelled
 ├─ step / token / wall-time budgets
 ├─ tool_calls[]
 ├─ approvals[]
 ├─ diff / snapshot
 └─ artifacts[]
```

ToolCall 记录：

```text
id, run_id, tool, args_json, risk, status,
started_at, ended_at, exit_code,
stdout_hash, stderr_hash, output_artifact_id,
diff, approval_id
```

### 3.3 运行时层

`Executor` 统一文件与命令：

```go
type Executor interface {
    Read(ctx, path string, offset, limit int) (FileChunk, error)
    Write(ctx, path string, content []byte) (Diff, error)
    Edit(ctx, path string, edits []Edit) (Diff, error)
    List(ctx, path string) ([]FileInfo, error)
    Glob(ctx, pattern string) ([]string, error)
    Grep(ctx, pattern, path string, limit int) ([]Match, error)
    Run(ctx, Command) (CommandResult, error)
    Git(ctx, GitOp) (GitResult, error)
}
```

关键点：

- 所有路径先 `resolve + clean`，必须落在 workspace root 内；拒绝 `..`、绝对路径逃逸、symlink 逃逸。
- `Run` 不接任意 shell 字符串，优先 `argv[]`；如果必须 shell，走受限 shell + 命令白名单/风险分类。
- 输出按字节截断，完整输出写 artifact；事件流只发摘要。
- 每次写文件前记录旧内容 hash，Run 结束生成统一 patch。

## 4. 权限矩阵

| 操作 | 默认 | 说明 |
|---|---|---|
| 读 workspace 文件 | 自动 | 只读 workspace；不读 `$HOME`、`/etc`、密钥 |
| 写 workspace 文件 | 自动 + diff | 保留旧版本/快照；UI 可查看 diff |
| 运行测试/构建 | 自动 | 白名单：`go test/build`、`npm test`、`pytest` 等 |
| 安装依赖 | 需批准 | `npm install`、`pip install`、`go get` 可能执行 lifecycle script |
| 联网 | 默认拒绝 | 需要显式 `network=true` 的 sandbox 配置 |
| 删除大量文件 | 需批准 | 路径/数量/体积阈值 |
| `git push/reset/clean` | 需批准 | 只允许本地 commit/branch 自动 |
| 越界路径 | 拒绝 | workspace 外一律拒绝，不弹批准 |
| 读取密钥/环境变量 | 拒绝/脱敏 | 不把宿主 env 注入；只给最小 env |
| 启动长时间进程 | 需批准 | 超时/CPU/内存限制 |

审批分级：

```text
auto       读、workspace 内写、白名单测试
ask        安装依赖、联网、删除、git 破坏性操作
deny       越界、宿主 shell、docker socket、密钥
```

## 5. 沙箱

### 默认：Docker

```text
docker run --rm \
  --network none \
  --read-only \
  --tmpfs /tmp:rw,noexec,nosuid,size=256m \
  --user 1000:1000 \
  --cap-drop ALL \
  --security-opt no-new-privileges \
  --pids-limit 256 \
  --memory 2g --cpus 2 \
  -v <workspace>:/workspace:rw \
  -w /workspace \
  <runner-image>
```

- 不挂 docker socket；不挂 `$HOME`；不传宿主 secrets。
- 需要联网时用单独的 network profile，或由宿主代理注入短期凭据。
- 每个 workspace 一个容器或一个长期容器池；Run 结束销毁/回收。
- 镜像里预装 Go/Node/Python/常用文档库，按场景选择镜像，避免运行中安装。

### 开发模式：LocalSandbox

- 只允许本机、单用户、显式 `EXECUTOR=local`。
- 强制 workspace 根目录，命令白名单，禁止 `sudo`、`ssh`、`curl`、`rm -rf /` 等。
- UI 明确标「本地开发模式，不具备生产隔离」。

### 可选：Pi RPC sidecar

- 在同一个沙箱里跑 `pi --mode rpc --no-session --tools read,write,edit,bash`，Go 通过 JSONL 通信。
- 优点：成熟工具、skills、compaction；缺点：Node 运行时、协议/会话映射、权限仍要外层包。
- 定位：P1 的「高级代码执行器」选项，不作为默认。

## 6. 代码工作室 vs 文档生成

### 代码工作室

1. 用户说需求。
2. 架构角色拆任务，operator 读取仓库、列文件、搜索。
3. operator 产出计划（哪些文件、怎么改、怎么测）。
4. 执行：read → edit → test → 读失败 → 再 edit，最多 N 步。
5. UI 显示文件 diff、测试输出、artifact；用户可 approve/revert。
6. 结束：本地 commit 到临时分支，生成 patch；不自动 push。

### 文档生成

- 不必给任意 shell；给 `docx/xlsx/pptx/pdf` 工具包，底层在沙箱里调用 Python 库。
- 输入：资料文件/网页/对话；输出：artifact + preview。
- 和代码场景共用 Workspace/Run/Artifact，只是 ToolPack 不同。
- 生成后返回文件卡片，用户下载；不把二进制塞进聊天消息。

## 7. API 草案

```text
POST   /v1/workspaces                  创建/导入 workspace（zip 或 git）
GET    /v1/workspaces/{id}/tree
GET    /v1/workspaces/{id}/file?path=
POST   /v1/workspaces/{id}/runs        创建任务
GET    /v1/runs/{id}/events            SSE：tool start/update/end、diff、artifact
POST   /v1/runs/{id}/approve
POST   /v1/runs/{id}/abort
GET    /v1/runs/{id}/diff
POST   /v1/runs/{id}/revert
GET    /v1/artifacts/{id}
```

- 所有接口按 `user_id` / `conversation_id` 鉴权；workspace 不跨用户。
- 同一 workspace 同时只允许一个 active Run；排队。
- Run 事件持久化，刷新后可恢复；不只依赖 SSE 内存。

## 8. 数据模型

```go
type Workspace struct {
    ID, UserID, ConversationID string
    Root string          // 沙箱内路径，不暴露宿主绝对路径
    Sandbox string       // docker | local | remote
    Status string
    CreatedAt time.Time
}

type Run struct {
    ID, WorkspaceID, ConversationID, UserID string
    Task string
    Status string
    Step, MaxSteps int
    StartedAt, EndedAt time.Time
    SnapshotID string
}

type Approval struct {
    ID, RunID, ToolCallID string
    Risk string          // install/network/delete/git
    Summary string
    Diff string
    Status string        // pending/approved/rejected/expired
}

type Artifact struct {
    ID, RunID string
    Path, MIME, SHA256 string
    Size int64
}
```

## 9. 分阶段落地

### P0（先做，能跑通代码工作室）

- Workspace 创建（zip 上传 + git clone）。
- Docker sandbox + 只读根 + workspace 挂载。
- `read/list/glob/grep/write/edit/run`。
- Run SSE 时间线、diff、snapshot/revert。
- 命令白名单 + 风险分类；安装/联网/删除审批。
- 一个 operator 角色调用工具；其他角色只读/建议。
- 预算：最多 20 步、10 分钟、2GB、2 CPU。

### P1

- `git` 工具（status/diff/commit/branch）。
- 测试结果结构化（失败用例/堆栈）。
- 文档工具包（docx/xlsx/pptx/pdf）+ artifact 预览。
- 审批 UI、运行历史、取消/重试。
- 可选 Pi RPC sidecar（容器内）。

### P2

- Skills/MCP：按场景加载工具包。
- 多角色 review：架构/测试/审校只读，operator 写。
- 远程 runner / 队列 / 配额。
- 记忆：只记决策、文件、测试结论和 artifact，不记全量 tool 输出。

## 10. 关键取舍

1. **不把 PI 直接当产品内核**：它是很好的参考和可选执行器，但默认无权限系统；我们的多用户产品必须自己包一层。
2. **不把 shell 字符串直接给模型**：优先结构化 argv；shell 仅作为受限兼容层。
3. **不让所有角色都有写权限**：operator 一个写，其他 review；减少并发冲突和误操作。
4. **不自动 push**：Run 结束只产生本地 commit/patch；push 是人工动作。
5. **不把二进制塞 DB/聊天**：artifact 独立存储、hash、下载/预览。

## 11. 需要产品确认的三个默认值

- Workspace 来源：默认 **zip 上传 + git clone**，不做宿主目录挂载。
- 默认运行时：**Docker**；本地模式只给开发机。
- 代码工作室默认：**workspace 内写自动 + 高风险命令审批**；不默认每次写都弹窗。

如果这三个默认值 OK，P0 可以直接按上面的接口和工具集落地。

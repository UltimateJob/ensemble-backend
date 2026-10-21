# 本机执行器与工作区设计

> 产品决策（2026-10-21）：
>
> 1. **工作区来源**：桌面端直接选本机目录；未来做大后支持云端操控 Git 仓库。
> 2. **运行时**：就在本地，参考 PI-agent 的本地 agent 模式，不默认上 Docker。
> 3. **权限**：每个 Agent 可单独设置——只读 / 执行命令需批准 / 完全访问。

目标：让代码工作室、文档生成真正能读写文件、执行命令、产出 diff 和 artifact，同时把「本地用户权限」和「Agent 权限」分开。

---

## 1. 结论

- **代码工作室必须有执行器**：只靠聊天模型改不了仓库。需要 `read/write/edit/list/grep/run`，还要能看 diff、跑测试。
- **执行器跑在本地**：桌面端 Go 进程以当前用户身份运行，工作区就是用户选中的本机目录。
- **本地运行时不是安全沙箱**：Agent 的「完全访问」本质上和 PI-agent 一样，拥有启动用户的权限。文件工具可以做 root 约束，但本地 shell 命令不能假装被 root 约束；命令风险分类、审批、快照和审计必须诚实说明边界。
- **权限按 Agent 配**：同一个群里，架构/审校可以只读，operator 可以「命令需批准」，只有用户明确授权的 Agent 才能「完全访问」。
- **写操作可回滚**：每次 Run 前做 snapshot；写入产生 diff；Run 结束可 revert。
- **命令权限要诚实**：`read/write/edit/list/grep` 这类文件工具会被 root 守卫限制；`run` 是本地进程，`full` 下等同于当前用户权限，可能访问工作区外。首版不把本地 shell 宣称为沙箱；要严格隔离就以后接 Docker/OS sandbox。

---

## 2. PI-agent 参考

源码已 clone 到 `research/pi`（研究目录，不进产品）。

值得借：

1. **Agent loop**：模型返回 tool calls → 执行 → tool result 回填 → 再请求模型，直到没有工具调用。
2. **工具语义**：
   - `read` 分页、图片、截断；
   - `write` 整文件写；
   - `edit` 精确文本替换，多个不重叠 edits，生成 diff/patch；
   - `bash` 支持 cwd、超时、取消、流式输出、截断，完整输出落盘。
3. **可插拔 operations**：文件/命令操作抽象成 `Operations`，可替换成本地、远程、容器实现。
4. **RPC/SDK**：JSONL 长驻子进程，适合 Go 后端跨语言集成；也有 TypeScript SDK。
5. **Session/Compaction/Skills**：会话树、上下文压缩、按需技能；我们已有记忆层，不需要照搬整棵树，但「原始事件保留、上下文可压缩」值得借。

不能照搬：

- PI 没有内置权限系统，默认以启动用户权限运行；项目信任只控制加载哪些资源，不限制工具访问。
- PI 的安全文档明确说：不要靠 transcript、项目信任、review 变化来建立安全边界。
- 我们做本地运行时，必须自己补上执行器层的权限与审计。

---

## 3. 分层

```text
┌────────────────────────────────────────────────────┐
│ 会话层（已有）                                      │
│ 角色 / 群聊 / 记忆 / 场景 / SSE                     │
│ 角色可以讨论、拆任务、review；不等于有执行权限       │
└──────────────────────┬─────────────────────────────┘
                       │ 用户确认「执行」
┌──────────────────────▼─────────────────────────────┐
│ 工作层（新增）                                      │
│ Workspace · Run · ToolCall · Approval · Artifact    │
│ AgentRunner：tool-call 循环 + 事件流 + 预算          │
│ Snapshot / Diff / Revert / Audit                    │
└──────────────────────┬─────────────────────────────┘
                       │ Executor 接口
┌──────────────────────▼─────────────────────────────┐
│ 运行时层（首版本地）                                │
│ LocalExecutor（当前用户权限 + 工作区根目录约束）     │
│ 可选：Docker / Remote / Pi RPC sidecar（以后）       │
└────────────────────────────────────────────────────┘
```

### 3.1 会话层

现有 `Orchestrator` 继续负责角色自主聊天。代码工作室里设一个 **operator** 角色负责执行，其他角色只读和 review。

角色在聊天里说「我来改」不等于真的执行；执行必须经过工作层的 Run + 权限检查。

### 3.2 工作层

一次任务是一个 Run：

```text
Run
 ├─ workspace_id
 ├─ conversation_id / user_id
 ├─ operator_agent_key
 ├─ status: queued | running | waiting_approval | done | failed | cancelled
 ├─ step / token / wall-time budgets
 ├─ tool_calls[]
 ├─ approvals[]
 ├─ diff / snapshot
 └─ artifacts[]
```

### 3.3 运行时层

统一 `Executor` 接口：

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

所有工具都走同一个 `Workspace`：

- `root` 是用户选中的本机目录；
- 所有路径 `Abs + EvalSymlinks + Rel` 校验，拒绝 `..`、绝对路径逃逸、symlink 逃逸；
- 不读 `$HOME`、`/etc`、密钥文件，除非 root 本身就是那个目录且用户明确授权；
- 命令默认使用最小环境变量，不继承宿主全部 env。

---

## 4. Agent 权限模型

### 4.1 三档权限

| 权限 | 读 | 写工作区 | 执行命令 | 说明 |
|---|---|---|---|---|
| `readonly` | 自动 | 拒绝 | 拒绝 | 架构、审校、测试分析用 |
| `approval` | 自动 | 自动 + diff | **每次需批准** | 默认 operator |
| `full` | 自动 | 自动 + diff | 自动 | 用户明确授权的 Agent |

补充：

- `approval` 的写操作默认允许，但会产生 diff 和 snapshot；如果用户希望写也审批，可以在 Workspace 上开 `writeApproval=true`。
- `full` 的文件工具仍受工作区根目录约束；但 `run` 是本地进程，命令本身可能访问工作区外。`full` 必须由群主二次确认，并在 UI 明确标注「等同当前系统用户权限」。
- 如果产品要求「命令也只能碰工作区」，本地模式需要 macOS `sandbox-exec`/Linux namespace 或 Docker；首版不做这个承诺。
- 删除大量文件、`git push/reset/clean`、安装依赖、联网，即使在 `full` 下也建议单独确认；这些是不可逆/高影响动作。
- 协作者不能把自己的权限改成 `full`；只有群主/工作区所有者能改。

### 4.2 默认角色权限

代码工作室：

- 架构：`readonly`
- 后端/前端 operator：`approval`（用户可改 `full`）
- 测试：`readonly`（跑测试命令需要 operator 或批准）
- 审校：`readonly`

文档工作室：

- 主笔：`approval`
- 检索/审校：`readonly`
- 成文/导出：`approval`

### 4.3 审批记录

```go
type Approval struct {
    ID, RunID, ToolCallID string
    AgentKey string
    Risk     string // run/install/network/delete/git/outside
    Summary  string
    Command  string
    Diff     string
    Status   string // pending/approved/rejected/expired
    DecidedBy string
    DecidedAt time.Time
}
```

审批选项：`允许一次`、`本次 Run 允许`、`本次会话允许`（只对同类命令）、`拒绝`。

---

## 5. 本地运行时

### 5.1 工作区来源

**首版：桌面端选择本机目录。**

- 桌面壳（Electron/Tauri）调用原生 folder picker，把绝对路径交给本地 Go 后端；
- 后端校验目录存在、可读、可写，记录 `Workspace.Root`；
- 浏览器模式没有原生 picker 时，提供「输入本机路径」输入框，仅允许 localhost 使用；
- 工作区路径只存在本地，不上传云端。

**未来：云端 Git。**

- `Workspace.Source = "git"`，记录 repo URL、branch、credential ref；
- 云端 runner clone 到隔离目录，执行、生成 patch/commit；
- 本地目录和云端 Git 共用同一套 Run/Tool/Approval/Artifact 模型。

### 5.2 本地执行的风险

本地运行时**没有 OS 隔离**，`full` Agent 能做的操作和当前用户一样多。必须承认这一点。

首版缓解：

- 文件工具的工作区根目录约束；
- 命令风险分类 + 审批（`approval` 每次看命令；`full` 二次确认后放行）；
- 写前 snapshot、写后 diff；
- 最小 env，不传密钥；
- 运行日志/审计，记录 agent、命令、退出码、diff；
- 高风险命令黑名单（`sudo`、`ssh`、`curl | sh`、`rm -rf /`、磁盘格式化、关机等）；
- 超时、输出上限、进程组清理；
- 明确告知：`full` 的本地命令不受工作区 root 约束；要强隔离需 Docker/OS sandbox。

以后需要更强隔离时，再把 `LocalExecutor` 替换成 Docker/VM，接口不变。

### 5.3 命令执行

- 优先 `argv[]`，不直接接任意 shell 字符串；
- 必须支持 shell 时，走受限 shell，记录完整命令；
- 默认工作目录 = workspace root；
- 默认超时 120s，可配置；
- stdout/stderr 流式回传，超过 256KB 截断，完整输出写 artifact；
- 取消时杀整个进程组，避免孙进程残留；
- 不继承宿主全部环境变量，只传 `PATH`、`HOME`（指向 workspace 临时 home）、`LANG` 等最小集合。

---

## 6. 代码工作室 vs 文档生成

### 代码工作室

1. 用户说需求。
2. 架构角色拆任务，operator 读取仓库、列文件、搜索。
3. operator 给出计划：改哪些文件、怎么测。
4. 执行：read → edit → test → 失败再 edit，最多 N 步。
5. UI 显示 tool 时间线、文件 diff、测试输出、artifact。
6. 结束只做本地 commit/生成 patch，**不自动 push**；用户可 approve/revert。

### 文档生成

- 共用 Workspace/Run/Artifact，只换工具包；
- 工具：`docx_create`、`xlsx_create`、`pptx_create`、`pdf_export`；
- 底层在本地调用 Python/Go 库，不把二进制塞聊天消息；
- 生成后返回文件卡片和预览。

---

## 7. API 草案

```text
POST   /v1/workspaces                  绑定本机目录 / 未来 git
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

- 所有接口按 `user_id` / `conversation_id` 鉴权；
- 同一 workspace 同时只允许一个 active Run；
- 事件持久化，刷新后可恢复；
- Agent 权限在创建 Run 时从会话角色配置读取，服务端强制执行。

---

## 8. 数据模型

```go
type AgentPermission string

const (
    PermissionReadonly AgentPermission = "readonly"
    PermissionApproval AgentPermission = "approval"
    PermissionFull     AgentPermission = "full"
)

type Workspace struct {
    ID, UserID, ConversationID string
    Source string        // local | git
    Root   string        // 本地绝对路径；不对外暴露给其他用户
    Status string
    CreatedAt time.Time
}

type Run struct {
    ID, WorkspaceID, ConversationID, UserID string
    OperatorAgentKey string
    Task string
    Status string
    Step, MaxSteps int
    StartedAt, EndedAt time.Time
    SnapshotID string
}

type ToolCall struct {
    ID, RunID, AgentKey, Tool string
    ArgsJSON string
    Risk string
    Status string
    ExitCode int
    OutputArtifactID string
    Diff string
    ApprovalID string
}
```

`ConvAgent` / `SavedAgent` 增加：

```go
Permission AgentPermission `json:"permission,omitempty"` // 缺省按场景默认
```

---

## 9. 分阶段落地

### P0（本地执行器）

- 桌面端选择本机目录 / 浏览器输入路径（localhost）。
- LocalExecutor：`read/list/glob/grep/write/edit/run`。
- 路径守卫、symlink 守卫、最小 env。
- Agent 权限三档，服务端强制执行。
- 命令审批：`approval` 每次弹批准；`full` 直接执行；`readonly` 拒绝。
- Run SSE + tool 时间线 + diff + snapshot/revert。
- 一个 operator 角色；其他角色只读。
- 预算：20 步、10 分钟、输出 256KB/工具。

### P1

- 文档工具包（docx/xlsx/pptx/pdf）+ artifact 预览。
- Git 工具：status/diff/commit/branch；push 仍需批准。
- 审批「本次 Run/会话允许」。
- 运行历史、取消/重试。
- 可选 Docker 执行器（同一接口）。

### P2

- 云端 Git workspace + 远程 runner。
- Skills/MCP 按场景加载。
- 多角色 review：架构/测试/审校只读，operator 写。
- 配额、审计导出、团队权限。

---

## 10. 关键取舍

1. **本地运行时不做假隔离**：它是用户权限，不是沙箱；权限靠执行器强制。
2. **不把 shell 字符串直接给模型**：优先 argv；shell 仅作兼容层。
3. **不让所有角色都有写权限**：operator 写，其他 review。
4. **不自动 push**：Run 结束只产生本地 commit/patch。
5. **不把二进制塞聊天/DB**：artifact 独立存储、hash、预览/下载。
6. **不把 Docker 作为首版默认**：本地运行优先；Docker 作为以后的隔离选项。

---

## 11. 已确认的默认值

- 工作区：**桌面端选本机目录**；未来加云端 Git。
- 运行时：**本地进程**，参考 PI-agent；Docker 以后可选。
- 权限：**每个 Agent 可选 `readonly` / `approval` / `full`**；服务端强制执行，不靠提示词。
- 写操作：**工作区内自动 + diff/snapshot**；命令按权限审批。
- `full`：**文件工具工作区内完全访问；本地命令等同当前系统用户权限**，需要群主二次确认；工作区 root 不约束 shell。

下一步就是 P0：先把 Workspace + LocalExecutor + 三档权限 + 审批/快照跑通，再接 operator 的 tool loop 和 UI。

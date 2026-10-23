# 项目协作约定（Claude Code）

本仓库有多个 Agent 并行开发。**开工前必读 `.claude/COORDINATION.md`**。

## 工作区

| Agent | Worktree | 分支 |
|---|---|---|
| 本地开发 Agent | `.claude/worktrees/local` | `wip/local-main` |
| 运维 Agent | `.claude/worktrees/ops` | `wip/ops` |

- 只在自己的 worktree 里改代码。
- 开工前 `git fetch origin && git rebase origin/main`。
- 完成后 push 自己的分支并开 PR 到 `main`，不直接推 `main`。

## 同步

- `.claude/COORDINATION.md` 是双方唯一的同步通道：改了共享文件先登记「正在改」；需要对方处理写进「已知问题」。
- 设计取舍写进 `docs/decisions.md`（入库）；服务器凭据只写 `README.local.md`（不入库，也不要在对话里复述）。

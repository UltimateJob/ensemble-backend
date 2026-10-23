# 工作层验收清单

> 每一条都能自己跑一遍。跑不通就是坏的。
>
> 前置：`make build`，服务在 `:8080` 跑着，有 `DEEPSEEK_API_KEY`（没有会降级到 mock，但数据没意义）。

## 0. 全量测试

```bash
go test ./...        # 9 个包
```

预期：全绿。工作层相关的是 `internal/{workspace,agent,docgen}`。

## 1. 工作区守卫

```bash
cd ensemble-backend
go test ./internal/workspace/ -run TestPathGuard -v
go test ./internal/workspace/ -run TestSymlinkEscape -v
```

预期：`../escape.txt`、`/etc/passwd`、指向外部的 symlink 全部被拒；正常路径可读。

## 2. 命令执行

```bash
go test ./internal/workspace/ -run "TestRunTimeout|TestRunEnvFiltersSecrets|TestRunCwdGuard" -v
go test ./internal/workspace/ -run TestDaemonCommandRejected -v
```

预期：
- `sleep 30` + 300ms 超时 → 进程组被杀，`exit 124`
- 子进程拿不到 `MY_API_KEY`，`HOME` 不是真实用户目录
- `pwd` 逃逸 cwd 被拒
- `nohup ./bin` 直接拒绝（执行器不负责常驻进程）

## 3. 权限三档 + audit

```bash
go test ./internal/agent/ -run "TestReadonlyDeniesWrite|TestApprovalAllowsFileWrite|TestAuditPermission" -v
```

预期：
- readonly 写文件被拒
- approval 写文件自动放行并产生 diff
- audit 能跑命令、不能写文件

## 4. scope 职责边界

```bash
go test ./internal/agent/ -run "TestScopeAllows|TestScopeBlocksOutOfBoundsWrite" -v
```

预期：`scope=["web/"]` 改 `main.go` → denied，写 `web/index.html` → ok。

## 5. 并行与文件锁

```bash
go test ./internal/agent/ -run "TestFileClaim|TestParallelRuns|TestParallelDiffFilteredByScope" -v
```

预期：
- 同文件第二个 claim 返回占用者
- scope 不相交的两个 Run 可同时跑，第四个被拒（上限 3）
- 并行时各自 diff 只含自己 scope 内的文件

## 6. 审批与回滚

```bash
go test ./internal/agent/ -run "TestApprovalRunWaitsForApproval|TestApprovalRejected|TestFullRunsCommandDirectly" -v
go test ./internal/workspace/ -run TestSnapshotDiffRevert -v
```

预期：approval 下命令挂起等批准；拒绝则不执行；full 直接执行；快照可回滚增/删/改三种状态。

## 7. 文档工具

```bash
go test ./internal/docgen/ -v
go test ./internal/agent/ -run "TestDocxToolWritesFile|TestDocxRejectsFabricatedNumbers" -v
```

预期：生成的 docx/xlsx 能被 `python-docx` / `openpyxl` 打开（装了才会跑，否则 skip）；编造的统计数字被拒。

## 8. 复审闭环

```bash
go test ./internal/agent/ -run "TestAutoReviewSpawnsAuditRun|TestAutoFixChain|TestAutoFixSkippedWhenClean|TestChainRollupIncludesReview" -v
```

预期：
- 主 Run 成功后自动起 audit 复审
- 复审 `VERDICT: issues` → 起修复 → 再复审，最多 2 轮
- `VERDICT: clean` → 不起修复
- chain 汇总包含所有子 Run

## 9. 持久化

```bash
go test ./internal/agent/ -run TestPersistence -v
```

预期：
- 重启后工作区和 Run 还在
- 在途 Run 标记 aborted 并说明原因
- 目录被删的工作区跳过，不影响其它

## 10. 真实链路（要 Key）

```bash
# 协作 vs 单 agent 对照
python3 scripts/collab_demo.py --case demo --root /tmp/ws1 --task "..."
python3 scripts/collab_demo.py --case demo --root /tmp/ws2 --solo --task "..."

# 带复审
python3 scripts/collab_demo.py --case demo --root /tmp/ws3 --selfreview --task "..."
```

预期：两个模式都能跑完，报告写到 `docs/collab-*.json`，包含每个 run 的 token / 工具数 / diff。

## 11. 界面

```bash
open desktop/build/纸团.app          # 原生窗口
# 或浏览器 http://localhost:8080/app/ → 点顶栏 </> 打开工作室
```

检查项：
- 选一个空目录 → 出现橙色提示「这个目录是空的」
- 填任务 → 「开始执行」→ 时间线逐步出现工具调用
- approval 权限下跑命令 → 出现审批卡片，点「允许一次」继续
- 结束后显示 diff 卡片 + 「回滚这次改动」
- 「执行完自动复审」默认勾选 → 主 run 结束后下面接一条复审时间线

## 12. 已知边界（不是 bug）

| 项 | 说明 |
|---|---|
| `full` 仍是本地用户权限 | 文件工具受工作区约束，shell 不受 |
| 并行上限 3 | 超过要排队 |
| 并行期间回滚被拒 | 避免连带别人的改动 |
| 整文件 `write` 可能覆盖 | `edit` 是安全的，`write` 靠先 read 兜 |
| pptx 未做 | docx / xlsx 已做 |
| 云端 Git 未做 | 只支持本机目录 |
| 协作不提速 | 见 [collab-benchmark.md](collab-benchmark.md) |

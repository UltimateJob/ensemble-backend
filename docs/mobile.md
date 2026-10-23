# 手机端（智团 Mobile）

Expo + React Native 的移动端。**手机配对电脑后，可以远程发指令、看工作区、审批、看产出**——执行仍然发生在电脑上。

---

## 一句话

手机不是电脑的克隆，是一条**受限通道**：只能操作已绑定的工作区，每台设备可单独吊销，默认每次执行前要审批（可关）。

---

## 跑起来

```bash
cd mobile
npm install
npx expo start          # 扫码用 Expo Go 打开，或按 i / a 开模拟器
```

后端要在同一网络可达：

```bash
# 仓库根目录
./scripts/dev.sh                     # 监听 0.0.0.0:8080
```

手机上填 `电脑局域网IP:8080`（macOS 用 `ipconfig getifaddr en0` 查）。

---

## 配对流程

```
电脑（浏览器/桌面端）              手机
  设置 → 配对新设备
        │
        ├─ 生成 6 位码（5 分钟、一次性）
        │       POST /v1/devices/pair-code    ← 仅 loopback
        │
        └────────── 码 ──────────▶ 输入码 / 扫码
                                        │
                                    POST /v1/devices/pair   ← 免登录
                                        │
                                   ◀── 设备令牌 dev_xxx
                                        │
                                   存 SecureStore（Keychain / Keystore）
```

### 为什么配对码只能在电脑上生成

拿到配对码 = 拿到一台能操作本机代码的设备。这个接口如果公网可调，等于把钥匙挂在门上。所以 `handleIssuePairCode` 保留 `isLoopback` 检查——手机上永远调不到它，这是故意的。

### 令牌怎么存的

- 库里只存 **SHA-256 哈希**，明文只给手机一次
- 手机存 `expo-secure-store`（iOS Keychain / Android Keystore），不是 AsyncStorage
- 吊销后服务端 `VerifyDevice` 立刻拒绝，不需要等令牌过期

---

## 能力边界

| 能做 | 不能做 |
|---|---|
| 看已绑定的工作区、文件树、文件内容 | 绑定新目录（依然只允许电脑上操作） |
| 发任务、看执行过程、看 diff | 拿到整台电脑的文件系统 |
| 审批 / 拒绝 / 本任务内放行 | 绕过 `critical` 命令黑名单 |
| 回滚到任务前快照 | 查看别人的工作区 |
| 发群聊消息、@角色 | — |

关键实现：`permissionFor` 是唯一权限入口，设备策略**只能加限制不能加权限**（[agent.go](../internal/agent/agent.go)）：

- `readonly` + 设备要求审批 → 写文件仍然被拒
- `full` + 设备要求审批 → 写文件和跑命令都要审批
- `critical` 命令 → 无论什么策略都拒

---

## 审批开关

每台设备有一个 `requireApproval`，**默认开**。

- 开：即使角色配了 `full`，写文件 / 跑命令前都会在手机上弹审批卡
- 关：按角色权限直接执行

这是用户自己的决定，服务端不替他做主；关闭时会记一条审计（`device.approval.off`），事后能查「谁在什么时候放宽了哪台设备」。

---

## 九个页面

| 页面 | 路由 | 数据来源 |
|---|---|---|
| 团队大厅 | `app/(tabs)/index.tsx` | `/v1/scenarios`、`/v1/conversations` |
| 群聊房间 | `app/team/[id].tsx` | `/v1/conversations/{id}/orchestrate`（SSE） |
| Agent 工作面板 | `src/components/AgentPanel.tsx` | `/v1/runs/{id}`、`/v1/workspaces/{id}/runs` |
| 目标看板 | `app/(tabs)/goals.tsx` | `/v1/goals` |
| 连接器 | `app/(tabs)/connect.tsx` | `/v1/connectors` |
| 资料库 | `app/(tabs)/library.tsx` | `/v1/library` |
| 新建团队 | `app/team/new.tsx` | `/v1/conversations` |
| 团队设置 | `app/settings/index.tsx` | `/v1/devices`、`/v1/conversations/{id}` |
| 审批弹窗 | `src/components/ApprovalSheet.tsx` | `/v1/runs/{id}/approve` |

### 状态环（设计包的灵魂）

头像上的四色环是这个产品和普通聊天框的最大区分点：

| 状态 | 颜色 | 含义 |
|---|---|---|
| `idle` | 灰 | 空闲 |
| `working` | 蓝色脉冲 | 后台在干活 / 正在发言 |
| `done` | 绿 | 刚完成（2 分钟窗口） |
| `blocked` | 红 | 待审批 / 上次失败 |

状态**全部派生**，没有独立的持久化状态机——避免两套状态互相打架。接口：`GET /v1/conversations/{id}/activity`。

---

## 视觉规范（硬约束）

来源：产品设计包的「前置统一基准」。违反任何一条都算实现错误。

```ts
// mobile/src/theme/tokens.ts
bg: '#FFFFFF'      // 纯白
card: '#F7F7F8'    // 次级卡片
accent: '#367BED'  // 低饱和柔和蓝
radius: 20         // 全部组件统一大圆角
shadow: 0 2px 10px rgba(0,0,0,0.06)
```

**禁止**：渐变、发光、霓虹、厚投影、暗色主题、<16px 圆角、3D 写实人像。

### 头像

设计包要求「黏土 3D 渲染」。实际用的是**确定性黏土风组件**（`src/components/Avatar.tsx`）：角色名 hash 取色 + 双层柔光边框 + 首字。

原因：角色来自后端（内置 61 个 + 用户自造 + 随机朋友），数量无上限，不可能预先生成 3D 图。同一个角色在任何页面永远同一颜色，视觉接近但不等同于渲染图。

---

## 后端新增

| 能力 | 接口 | 存储 |
|---|---|---|
| 设备配对 | `POST /v1/devices/pair-code`（loopback）、`POST /v1/devices/pair`（免登录）、`GET/DELETE/PATCH /v1/devices/{id}` | `data/devices.json`（只存哈希） |
| 状态环 | `GET /v1/conversations/{id}/activity` | 派生，不落盘 |
| 目标看板 | `GET/POST/PATCH/DELETE /v1/goals` | `data/goals.json` |
| 连接器 | `GET/POST/PATCH/DELETE /v1/connectors`、`POST /v1/connectors/{id}/probe` | `data/connectors.json`（凭据加密） |
| 资料库 | `GET /v1/library`、`GET/DELETE /v1/library/{id}` | `data/artifacts.json` |
| Run 审批查询 | `GET /v1/runs/{id}` 现在带 `approvals` | — |

### 连接器凭据加密

用 `JWT_SECRET` 派生 AES-256-GCM 密钥（`internal/store/secrets.go`）：

- 数据目录或备份包被单独拿走 → **解不开**
- 密钥必须在 `NewFileStore` **之前**注入，否则加载时用兜底密钥，所有连接器变「解密失败」（有回归测试锁住这个顺序）
- 改 `JWT_SECRET` 后已存凭据解不开，连接器标为 error 并提示重新授权——刻意的取舍

### 连接器的能力边界

**做到**：真实凭据存储 + 真实连通性测试（GitHub `/user`、Notion `/v1/users/me`、飞书 `tenant_access_token`、CalDAV GET）。

**没做**：各家的一键 OAuth 授权页。第一版填的是各自的 Token / App 凭据。

一个只会亮绿灯的假开关比没有更糟，所以「测试连接」会真的发一次鉴权请求，失败时把上游错误原文带回来。

---

## 验证

```bash
# 后端
go test ./... -count=1

# 手机端
cd mobile && npx tsc --noEmit
npx expo export --platform web --output-dir /tmp/zt-web   # 打包冒烟
```

新增测试覆盖：配对码过期 / 重放 / 试满作废 / 每用户上限、设备吊销立即失效 / 越权吊销、账号停用连带设备失效、凭据加密落盘 + 重启解密、审批开关的四个安全边界（不能放宽 readonly、不能绕过 critical 黑名单）。

---

## 已知边界

1. **必须同一网络**（或自行做端口转发）。后端没有内置 TLS，公网部署需要反向代理。
2. **iOS ATS / Android cleartext** 已通过 `app.json` 放开（`NSAllowsLocalNetworking`、`usesCleartextTraffic`），仅适用于开发；上线前应改成 HTTPS。
3. **连接器不注入 Agent 工具链**：目前是「存凭据 + 测连通」，还没把飞书/Notion 的读写能力接进执行器。这是下一步。
4. **审批靠轮询**（4 秒）：低频事件，轮询比给每个 Run 常驻 SSE 更省电。真需要实时可改推送。

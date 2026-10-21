# 合奏 Ensemble

> 五个声部，一个成品。

一个多角色群聊的 Agent 工作台。每个群是一组角色，它们自己判断要不要说话、可以互相接话、缺信息时直接问你。真跑了 DeepSeek。

![群聊](docs/chat.png)

---

## 30 秒跑起来

```bash
git clone https://github.com/UltimateJob/ensemble-backend
cd ensemble-backend
DEEPSEEK_API_KEY=sk-... ./scripts/dev.sh
```

没有 Key 也能跑——会自动降级到 mock 上游，输出是固定演示文本，但整条链路（登录 → 建群 → 流式对话 → 落盘）完全一致。

| 地址 | 说明 |
|---|---|
| http://localhost:8080/app/ | 客户端 |
| http://localhost:8080/admin/ | 运维台（token `admin-dev-token`） |
| http://localhost:8080/healthz | 健康检查 |

其他命令：

```bash
make run      # 直接跑
make build    # 编译到 bin/
make release  # 交叉编译 linux / darwin / windows
make reset    # 清空本地数据
```

---

## 它跟普通聊天机器人的区别

**一、谁想说谁说**

不是轮流点名。每个角色拿到完整上下文后自己判断：这个话题跟我有关吗？有关就开口，无关就回 `[PASS]`，群里留一条极轻的痕迹。

实测（无人触发，`kickoff` 自己聊起来的）：

> 葵：话说凛最近是不是又在折腾什么新东西，好几天没冒泡了。
> 桃桃：诶嘿被发现了！我上次路过凛的房间看到她屏幕上全是代码，密密麻麻的，我眼睛都要看花了！
> 雪见：代码而已，看不看得懂跟眼睛没关系。
> 凛：编译通过之前，谁也不知道自己看不看得懂。

四个人三个来回，没有一个是按顺序点的。雪见和凛还在互相呛。

**二、可以主动开口**

不需要用户先发消息。群建好就会自己聊起来，冷场时第一个人被强制要求起个头。

**三、缺信息会问你**

用户说「帮我把那个标做了」：

> 交付：@用户 哪个标？把招标文件发进来，我才能判断包什么、缺什么、要谁确认。

而且这一轮**只有交付说话了**——其他四个人都沉默，因为信息不足没法开工。前端把这条标成「在问你」，停下等回复。

**四、角色之间的纠错**

竞标场景里，工头说「案例要凑满 3 个同类三甲」，审校立刻拦下来：

> 审校：案例那块别「凑满」。招标写的是「同类三甲案例」，凑数容易被认定虚假响应，有废标风险。

研报场景里，检索报了「46GWh 估算值」，交叉验证直接否掉：

> 交叉验证：21.5GW 对 46GWh，折合约 2.1 小时，偏短……46GWh 是估算值还是终值？估算值不能进同比。

这是多 Agent 相对单模型的价值——**有人只负责挑错**。

---

## 自己造角色

![造角色](docs/forge.png)

给个名字和几个关键词，模型写出完整人设。实测生成：

> **夜岚** / 孤夜冷剑客
> 你说话极简，一句常不超过十字，语气冷硬无起伏。习惯用鼻音轻哼应答。绝不主动闲聊，不解释、不笑、不道歉。

拿三个现造的角色开一个群，`kickoff` 之后：

> **老周**（退休教师）我跟你们说啊，这天气一凉下来，人就容易犯懒，我这两天早上遛弯都起不来了，你们那边早晚凉不凉？
> **林小满**（巷口小卖部老板娘）哎我跟你说，凉！早上我五点半起来进货，那风一吹，我都要把秋裤翻出来了，你说这才几月份啊
> （**夜岚**，冷面剑客，这一轮没说话）

林小满主动把自己「五点半进货」的身份带进了对话——这个细节只在关键词里写了「开小卖部」，是她自己长出来的。

---

## 五个内置场景

| 场景 | 角色 | 用来干什么 |
|---|---|---|
| 竞标工作室 | 工头·查证·撰写·审校·交付 | 读招标文件、出合规点对表、挑废标风险 |
| 代码工作室 | 架构·后端·前端·测试 | 定边界、并行实现、找边界条件 |
| 研报工作室 | 主笔·检索·交叉验证·成文 | 每个数字可溯源 |
| 二次元茶话会 | 雪见·桃桃·凛·葵 | 毒舌学姐 / 元气学妹 / 技术宅 / 温柔调和者 |
| 日常陪聊 | 林·周·陈 | 话少靠谱 / 热闹 / 稳重倾听 |

---

## 架构

```
        ┌──────────────┐
 手机端 │              │      ┌──────────┐
        │   HTTP 层    │─────▶│ Service  │
  PC 端 │  httpapi     │      │ 业务用例  │
        │              │      └────┬─────┘
        └──────┬───────┘           │
               │                   ▼
               │            ┌──────────────┐
               │            │    Store     │ 接口 + 内存 + JSON 落盘
               │            └──────────────┘
               ▼
        ┌──────────────────────────────┐
        │         Gateway              │
        │  选路 · 熔断 · 重试 · 限流 · 计量 │
        └───────┬──────────────┬───────┘
                │              │
        ┌───────▼──────┐ ┌─────▼──────┐
        │  DeepSeek    │ │    Mock    │  Provider 接口
        └──────────────┘ └────────────┘
```

依赖单向：`httpapi → service → gateway/store → domain`。换数据库只实现 `store.Store`。

| 层 | 目录 | 职责 |
|---|---|---|
| HTTP | `internal/httpapi` | 路由、中间件、校验、SSE |
| 业务 | `internal/service` | 登录、会话、编排、场景、角色生成 |
| 网关 | `internal/gateway` | 模型选路、熔断、重试、限流、计量 |
| 存储 | `internal/store` | 接口 + 内存 + 文件持久化 |
| 领域 | `internal/domain` | 纯数据结构 |

### 编排器

`internal/service/orchestrator.go` 是整个产品的心脏：

1. 用户发言或 `kickoff` 触发
2. 随机挑一个没说过话的角色（避免连续同一个人）
3. 把完整历史 + 本轮进展塞进 prompt，问它「要不要说话」
4. 回 `[PASS]` 就跳过，说内容就流式推给前端
5. 如果它在问用户，停下等回复
6. 最多 `sc.Round` 个人开口

流式输出先缓冲 6 个字符，确认不是 `[PASS]` 再开始推——否则用户会看到 `[PA` 闪一下。

### 网关

```go
type Provider interface {
    Name() string
    Chat(ctx context.Context, req domain.ChatRequest) (<-chan domain.Chunk, error)
}
```

新增厂商只需实现这个方法 + 在管理台加一行模型（不用重启）。

熔断：连续失败达阈值 → `open` → 超时转 `half-open` 放一个试探 → 成功恢复。
限流：每用户令牌桶，闲置 30 分钟自动回收。
降级：无 Key 自动注册 mock 上游。

---

## 接口

### 公开

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/healthz` `/readyz` | 探针 |
| POST | `/v1/auth/send-code` | 发验证码（首版回显） |
| POST | `/v1/auth/login` | 手机/邮箱，自动建号 |
| GET | `/v1/scenarios` | 内置场景与角色 |

### 需要登录

| 方法 | 路径 | 说明 |
|---|---|---|
| GET/PATCH | `/v1/me` | 当前用户 / 改资料 |
| POST | `/v1/events` | 上报行为 |
| GET/POST | `/v1/conversations` | 会话列表 / 新建（可带自定义角色） |
| GET/DELETE | `/v1/conversations/{id}` | 详情 / 删除 |
| GET | `/v1/conversations/{id}/messages` | 历史消息（`?page=1&before=<id>&limit=30` 走游标分页） |
| POST | `/v1/conversations/{id}/orchestrate` | **用户发言**，角色自主决定接不接 |
| POST | `/v1/conversations/{id}/kickoff` | **无人触发**，让群里自己聊 |
| POST | `/v1/agents/draft` | 生成角色人设 |
| POST | `/v1/chat/completions` | 网关直连 |

### 管理（`X-Admin-Token`）

总览、模型 CRUD、用户启停与额度、熔断重置、审计日志、行为记录。

---

## 界面

| 桌面 | 移动 |
|---|---|
| ![桌面](docs/chat.png) | ![移动](docs/mobile.png) |

三栏：会话 / 消息流 / 成员·流水线·产物。移动端收敛为单栏 + 抽屉切群。

角色头像由名字 hash 生成渐变底色 + 首字，无外部图片依赖。
图标用 [Lucide](https://lucide.dev)（MIT），46 个内联进 `icons.js`，离线可用。

---

## 配置

全部环境变量，见 `.env.example`。

| 变量 | 默认 | 说明 |
|---|---|---|
| `ADDR` | `:8080` | 监听地址 |
| `ADMIN_TOKEN` | `admin-dev-token` | 运维台令牌 |
| `JWT_SECRET` | `change-me-in-production` | 会话签名 |
| `DEEPSEEK_API_KEY` | 空 | 留空启用 mock |
| `DATA_DIR` | `data` | 数据目录 |
| `RATE_PER_MINUTE` | `120` | 每用户每分钟请求 |
| `MAX_RETRIES` | `2` | 上游重试 |
| `BREAKER_ERRORS` | `5` | 熔断阈值 |
| `UPSTREAM_TIMEOUT` | `120s` | 上游超时 |

---

## 数据

按实体分片，不是一个巨型 JSON：

```
data/
  users.json           用户
  conversations.json   会话
  audits.json          审计
  events.json          行为事件
  messages/
    c_xxx.json         每个会话的消息独立一个文件
```

为什么分片：写一条消息只重写**这一个会话**的文件。整库单文件的话，聊到第 500 条时每发一句都要序列化全部历史。

写入策略：

- 写操作标脏，400ms 合并落盘
- 先写 `.tmp` 再 `rename`，避免进程被杀时留下半截文件
- 优雅关闭时最后 Flush
- 会话 ID 会过 `safeName` 过滤，防止路径穿越

从旧版单文件（`data/ensemble.json`）升级时会自动迁移，迁移完把原文件改名成 `.migrated` 留底。

```bash
make reset   # 清空全部数据
```

---

## 测试

```bash
go test ./... -race              # 全部用例（含竞态检测）
go test ./... -coverpkg=./internal/...
```

覆盖率 **61%**，核心逻辑接近全测：

| 模块 | 覆盖 |
|---|---|
| `pickSpeaker` 选人 | 100% |
| `isPass` 沉默判定 | 100% |
| `ModelTable.Pick` 加权选路 | 96% |
| `Registry.Report` 熔断计数 | 96% |
| `Limiter.Allow` 令牌桶 | 94% |

测试覆盖的行为：

- **谁能说话**：已说过的人不再选、上一轮发言者不连任、全部说过返回 nil
- **沉默判定**：`[PASS]`、`pass`、`[PASS] 我不懂`、`（无）` 都算沉默；`我 pass 掉这个方案` 算说话
- **流式缓冲**：`[PA`、`PAS` 这些不完整前缀要被识别为疑似沉默，不能推流
- **持久化**：写入 → 关闭 → 重开，用户/会话/消息都还在
- **跨用户隔离**：B 读不到 A 的会话，列表里也不出现
- **接口不泄露**：场景接口不能把角色的系统提示词返回给前端
- **端到端**：orchestrate 的 SSE 事件序列完整，start/end 配对，不发散超过场景上限

### 消息分页

首屏只拉 30 条，滚到顶部点「加载更早」再拉 30 条。游标是消息 ID 而不是 offset——offset 在并发插入时会错位。

```
GET /v1/conversations/{id}/messages?page=1&limit=30
  → { messages: [...], hasMore: true }

GET /v1/conversations/{id}/messages?page=1&limit=30&before=m_0a92...
  → 更早的 30 条
```

加载更早时会记录插入前后的 `scrollHeight` 差，把滚动位置顶回去，视觉上不跳。

### 浏览器端到端

```bash
node scripts/e2e.mjs          # 桌面 1440
node scripts/e2e-mobile.mjs   # 移动 390
```

不依赖 Playwright：直接用 CDP 连 headless Chromium，Node 内置 WebSocket 就够。

| 套件 | 断言 | 覆盖 |
|---|---|---|
| 桌面 | 22 | 登录 → 场景 → 建群 → 发消息 → 流式回复 → 刷新恢复 |
| 移动 | 13 | 390 视口、三栏收敛、抽屉开关、无横向溢出、窄屏流式 |

移动端那套用 `Emulation.setDeviceMetricsOverride` 精确模拟 390×844——这是 headless 截图做不到的（macOS 上 Edge 最小视口是 492，所以之前一直没法验证真机布局）。

断言里有几条是硬指标：

- `documentElement.scrollWidth - innerWidth <= 1`：任何横向溢出直接失败
- 输入框 `getBoundingClientRect().bottom` 不能超出屏幕
- 场景网格必须是 1 列

CI 在 `.github/workflows/ci.yml`：格式检查 → vet → 竞态测试 → 覆盖率 → 构建 → 冒烟 → 桌面 E2E → 移动 E2E。

写测试时抓到两个真 bug：带解释的 `[PASS] 这个话题我不懂` 被当成发言推给了用户；缓冲阶段 `[PA` 没被识别，用户会看到半截标记。都已修。

## 已知限制

上生产前必须处理：

1. **存储是单机 JSON 文件** —— 没并发控制，不能多实例。接 PostgreSQL 只需实现 `store.Store`
2. **验证码固定 `123456` 并回显** —— 接短信网关后移除
3. **限流与熔断是进程内状态** —— 多实例需接 Redis 共享
4. **没有鉴权分级** —— 所有登录用户权限相同，没有团队/空间概念
5. **P95 延迟是均值估算** —— 需要接入直方图
6. **只覆盖了逻辑层** —— UI 和真实上游没有自动化测试

---

## 目录

```
cmd/server          入口、依赖装配、优雅关闭
internal/config     环境变量
internal/domain     数据结构
internal/gateway    Provider / 熔断 / 限流 / 模型表
internal/service    auth / chat / orchestrator / scenario / draft
internal/store      接口 + 内存 + 文件持久化
internal/httpapi    路由 / 中间件 / handler
web/app             客户端（单页，无构建）
web/admin           运维台
scripts/dev.sh      一键启动
```

---

## 许可

原型项目，仅用于演示。

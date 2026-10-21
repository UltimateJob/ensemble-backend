# 合奏 Ensemble · 后端 + 客户端

![群聊](docs/chat.png)



Go 1.24，零第三方依赖。为多模型 Agent 工作台提供网关与业务接口，附运维管理台。

```
go run ./cmd/server
```

- 服务：http://localhost:8080
- 管理台：http://localhost:8080/admin/ （令牌 `admin-dev-token`，或 `?token=` 直接进）
- 未配置 `DEEPSEEK_API_KEY` 时自动启用 mock 上游，整条链路可离线跑通

## 现在能跑什么

```bash
export DEEPSEEK_API_KEY=sk-...
go run ./cmd/server
```

- 客户端：http://localhost:8080/app/
- 运维台：http://localhost:8080/admin/

真实打通：登录 → 选场景 → 建群 → 发一句话 → 群里多个角色依次用 DeepSeek 回复（SSE 流式），后一个角色能看到前一个说了什么。

五个内置场景：

| 场景 | 角色 | 说明 |
|---|---|---|
| 竞标工作室 | 工头·查证·撰写·审校·交付 | 读招标、出点对表、挑废标风险 |
| 代码工作室 | 架构·后端·前端·测试 | 定边界、并行实现、找边界条件 |
| 研报工作室 | 主笔·检索·交叉验证·成文 | 每个数字可溯源 |
| 二次元茶话会 | 雪见·桃桃·凛·葵 | 毒舌学姐 / 元气学妹 / 技术宅 / 温柔调和者 |
| 日常陪聊 | 林·周·陈 | 话少靠谱 / 热闹 / 稳重倾听 |

### 群聊是自主的，不是轮流发言

编排器不按固定顺序点名。每一轮：

1. **谁想说谁说** —— 每个角色独立判断这个话题跟自己有没有关系，无关就回 `[PASS]`，群里会留下一条「XX 没接话」的轻痕迹
2. **可以主动开口** —— `kickoff` 让群里自己聊起来，不需要用户先说话；第一个人被强制要求起头，避免整轮冷场
3. **可以直接问用户** —— 缺关键信息时角色会说「@用户」，前端把这条标成「在问你」，并停下等回复
4. **共享同一份上下文** —— 后说话的人能看到前面所有人说了什么，所以会互相接话、反驳、补刀

实测片段（DeepSeek 真实输出，无人触发，`kickoff` 自己聊起来的）：

> 葵：话说凛最近是不是又在折腾什么新东西，好几天没冒泡了。
> 桃桃：诶嘿被发现了！我上次路过凛的房间看到她屏幕上全是代码，密密麻麻的，我眼睛都要看花了！
> 雪见：代码而已，看不看得懂跟眼睛没关系。
> 凛：编译通过之前，谁也不知道自己看不看得懂。

用户说「我明天要考试，好紧张」之后：

> 凛：先睡够，缓存不清理第二天更卡。
> 葵：雪见这话说得对，不过考前一晚躺着干紧张也难受，要不要先把最没把握的那块过一遍再睡？
> 桃桃：诶嘿那我也算有经验的人了吧！我考砸过好几次但还是活蹦乱跳的，所以别太怕啦！
> （雪见这一轮选择沉默）

每个角色有独立的性格提示词。

**实测片段**（全部为 DeepSeek 真实输出）：

- 二次元：`凛：甜的午饭…甜品不算正餐，缓存命中率低。`
- 竞标：`审校：案例那块别「凑满」。招标写的是「同类三甲案例」，凑数容易被认定虚假响应，有废标风险。`
- 研报：`交叉验证：21.5GW 对 46GWh，折合约 2.1 小时，偏短，得确认是否含压缩空气、液流这些长时项目。`
- 代码：`测试：边界我列几个：上传 0 字节文件、xlsx 里只有空 sheet、表头有重复列名…`

角色之间会互相纠错——这是多 Agent 相对单模型的价值所在。

## 架构

```
              ┌──────────────┐   ┌──────────────┐
   手机端 ────▶│              │   │              │
              │   HTTP 层    │──▶│   Service    │
     PC 端 ───▶│  httpapi     │   │  业务用例     │
              │              │   └──────┬───────┘
              └──────┬───────┘          │
                     │                  ▼
                     │           ┌──────────────┐
                     │           │    Store     │  接口 + 内存实现
                     │           └──────────────┘
                     ▼
              ┌──────────────────────────────┐
              │         Gateway              │
              │  选路 · 熔断 · 重试 · 计费     │
              └───────┬──────────────┬───────┘
                      │              │
              ┌───────▼──────┐ ┌─────▼──────┐
              │  DeepSeek    │ │    Mock    │   Provider 接口
              └──────────────┘ └────────────┘
                      │
              ┌───────▼──────┐
              │  其他厂商…    │  实现 Chat() 即可接入
              └──────────────┘
```

### 分层

| 层 | 目录 | 职责 |
|---|---|---|
| HTTP | `internal/httpapi` | 路由、中间件、参数校验、SSE |
| 业务 | `internal/service` | 登录、会话、对话、运维用例 |
| 网关 | `internal/gateway` | 模型选路、熔断、重试、限流、计量 |
| 存储 | `internal/store` | 接口 + 内存实现 |
| 领域 | `internal/domain` | 纯数据结构，无依赖 |

依赖方向单向：`httpapi → service → gateway/store → domain`。换数据库只实现 `store.Store`。

## 网关设计

**Provider 接口**——新增厂商只需实现一个方法：

```go
type Provider interface {
    Name() string
    Chat(ctx context.Context, req domain.ChatRequest) (<-chan domain.Chunk, error)
}
```

**选路**：模型表里每个模型带 `provider`、`weight`、`enabled`。请求指定 `model` 时精确匹配，否则按权重在启用模型间加权轮询。

**熔断**：连续失败达阈值 → `open`，期间直接拒绝；超时后转 `half-open` 放一个试探请求；成功则恢复。管理员可在页面手动重置。

**重试**：`open` 之外的上游错误最多重试 `MAX_RETRIES` 次。首块成功即视为链路可用，后续中断交由客户端处理。

**限流**：每用户令牌桶，按分钟补充，闲置 30 分钟自动回收。

**降级**：无 Key 时注册 mock 上游，保证本地与 CI 可跑通全流程。

## 接口

### 公开

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/healthz` | 存活探针 |
| GET | `/readyz` | 就绪探针，含上游数 |
| POST | `/v1/auth/send-code` | 发验证码（首版回显 `devCode`） |
| POST | `/v1/auth/login` | 手机/邮箱登录，不存在自动建号 |

### 需要登录（`Authorization: Bearer <token>`）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/v1/me` | 当前用户 + 剩余额度 |
| PATCH | `/v1/me` | 改姓名/头像 |
| GET | `/v1/me/events` | 我的行为记录 |
| POST | `/v1/events` | 上报互动事件 |
| GET | `/v1/conversations` | 会话列表 |
| POST | `/v1/conversations` | 新建会话 |
| GET | `/v1/conversations/{id}` | 会话详情 |
| DELETE | `/v1/conversations/{id}` | 删除会话 |
| GET | `/v1/conversations/{id}/messages` | 历史消息 |
| POST | `/v1/conversations/{id}/messages` | 发消息，**SSE 流式返回** |
| POST | `/v1/chat/completions` | 网关直连，可流式或整包 |
| POST | `/v1/conversations/{id}/orchestrate` | 用户发言后，角色自主决定接不接话 |
| POST | `/v1/conversations/{id}/kickoff` | 没人说话时，让群里自己聊起来 |
| POST | `/v1/agents/draft` | 用模型生成角色设定 |

### 场景

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/v1/scenarios` | 内置场景与角色定义 |

### 管理（`X-Admin-Token`）

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/admin/api/login` | 校验令牌 |
| GET | `/admin/api/overview` | 指标 + 上游 + 用量 |
| GET/POST | `/admin/api/models` | 模型表读写 |
| DELETE | `/admin/api/models/{key}` | 删除模型 |
| GET | `/admin/api/users` | 用户列表 |
| POST | `/admin/api/users/{id}/status` | 启用/停用 |
| POST | `/admin/api/users/{id}/quota` | 调整额度 |
| POST | `/admin/api/providers/{name}/reset` | 手动恢复熔断 |
| GET | `/admin/api/logs` | 操作审计 |
| GET | `/admin/api/events` | 用户行为 |

## 配置

全部走环境变量，有默认值。

| 变量 | 默认 | 说明 |
|---|---|---|
| `ADDR` | `:8080` | 监听地址 |
| `ADMIN_TOKEN` | `admin-dev-token` | 管理台令牌 |
| `JWT_SECRET` | `change-me-in-production` | 会话签名密钥 |
| `DEEPSEEK_API_KEY` | 空 | 留空则启用 mock |
| `DEEPSEEK_BASE_URL` | `https://api.deepseek.com` | 上游地址 |
| `DEEPSEEK_MODEL` | `deepseek-chat` | 默认模型 |
| `RATE_PER_MINUTE` | `120` | 每用户每分钟请求数 |
| `MAX_RETRIES` | `2` | 上游重试次数 |
| `BREAKER_ERRORS` | `5` | 熔断阈值 |
| `BREAKER_OPEN` | `20s` | 熔断保持时长 |
| `UPSTREAM_TIMEOUT` | `120s` | 上游超时 |
| `SHUTDOWN_GRACE` | `20s` | 优雅关闭等待 |
| `ALLOW_MOCK` | `true` | 无 Key 时是否允许 mock |

## 高可用

- **优雅关闭**：收到 SIGTERM 后停止接收新连接，等待在途请求完成
- **panic 隔离**：中间件捕获，进程不退出
- **连接池**：HTTP/2、200 空闲连接、90s 回收
- **超时分层**：读 15s / 写 5min（长流）/ 上游 120s
- **熔断与降级**：上游故障不影响进程，返回结构化错误
- **限流**：按用户维度隔离，单账号打不满上游

## 扩展到多厂商

```go
// 1. 实现接口
type Qwen struct{ /* ... */ }
func (q *Qwen) Name() string { return "qwen" }
func (q *Qwen) Chat(ctx context.Context, req domain.ChatRequest) (<-chan domain.Chunk, error) { /* ... */ }

// 2. 注册
reg.Add(&Qwen{})

// 3. 模型表加一行（可在管理台加，无需重启）
//    { Key: "qwen-max", Provider: "qwen", Weight: 5, Enabled: true }
```

## 目录

```
cmd/server          入口、依赖装配、优雅关闭
internal/config     环境变量
internal/domain     数据结构
internal/gateway    Provider / 注册表 / 熔断 / 限流 / 模型表
internal/service    auth / chat / admin
internal/store      接口 + 内存实现
internal/httpapi    路由 / 中间件 / handler
web/admin           管理台单页
```

## 当前限制

- 存储为进程内内存，重启即清空；接口已抽出，接 PostgreSQL 只需实现 `store.Store`
- 验证码固定 `123456` 并回显，接短信网关后移除
- P95 延迟按均值估算，接入直方图后替换
- 未做多实例共享状态（限流、熔断为进程内）；生产需接 Redis

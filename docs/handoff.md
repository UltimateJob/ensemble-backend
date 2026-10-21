# 交接

如果明天有人接手这个项目，他需要知道这些。

---

## 一句话状态

**可以演示、可以试用、可以改。不适合直接对外运营。**

功能是完整的，边界是清楚的。缺的不是功能，是运营基础设施（见下文「已知边界」）。

---

## 什么是验证过的

每条都附证据，不是「应该没问题」。

### 真实 LLM 链路

| 验证项 | 怎么验的 |
|---|---|
| 五个场景的真实输出质量 | `make quality` — 统计每场发言人数、沉默人次、接话次数。当前：平均 2.7 人发言 / 接话 1.6 处 |
| 角色人设还原度 | 人工读。雪乃会反问、结衣先道歉、工头会拆步骤 |
| 接话而非各说各的 | 质量脚本的「接话」指标 > 0；`docs/chat.png` 里三个角色在互相质疑 |
| 额度与 token 统计 | 压测：12 轮 × 3 角色 = 额度 +36，消息 49 条，完全对齐 |

### 稳定性

| 验证项 | 怎么验的 |
|---|---|
| 长时间运行不漏内存 | `make soak` — 40 轮后内存稳定在 17MB |
| 20 人并发不串数据 | `make concurrent` — 9 项：跨用户隔离、同会话串行化、额度一致 |
| 断线不丢消息、不白扣额度 | `make resilience` — 10 项 |
| 同一账号多标签页 | `make multi-tab`（`make e2e` 里跑）— 8 项 |
| 从零部署能跑起来 | 重新 clone 到干净目录，只按 README 走 |

### 界面

| 验证项 | 怎么验的 |
|---|---|
| 桌面主流程 | E2E 47 项 |
| 390 视口 | 移动 E2E 13 项（CDP 设备模拟，不是窗口缩放） |
| 键盘导航 | 8 项（CDP 发真实按键） |
| 每个可点元素背后有活逻辑 | `make audit` — 24 按钮 / 5 菜单项 / 19 图标 |
| 新用户能不能用起来 | `make walk` 三轮走查，抓到 7 个问题 |

---

## 什么是没验证过的

诚实列出，别当成已解决。

| 项 | 风险 |
|---|---|
| **真实用户长期使用** | 所有测试都在几小时内。没跑过一周连续使用 |
| **大数据量** | JSON 分片在 10 万条消息以上会退化，没实测过临界点 |
| **移动端真机** | 只做了 CDP 设备模拟。真机的安全区、软键盘顶起、iOS Safari 差异都没验 |
| **跨浏览器** | 只在 Edge（Chromium）上跑过。Firefox / Safari 没测 |
| **恶意输入** | prompt 注入、超长消息、特殊字符没做压力测试 |
| **并发写同一个群** | 有会话锁，但锁的临界区边界没做过 fuzz |
| **金额相关** | 没有计费，所以也没有对账验证 |

---

## 已知边界

这些是**设计决定**，不是待办。要改的话先读 `docs/decisions.md`。

1. **单实例** — `flock` 独占 `data/.lock`，第二个实例启动即失败。要支持多副本得换存储和分布式锁
2. **JSON 分片存储** — 零依赖的代价。`docs/decisions.md` 有取舍说明
3. **进程内限流和熔断** — 重启即重置
4. **没有计费** — 额度是固定值，没有充值入口
5. **短信通道是骨架** — `SmsSender` 未接真实服务商，生产必须配
6. **`DEV_BYPASS` 默认开** — 演示账号免验证码。生产必须设 `false`
7. **没有账号找回** — 只有手机号 + 验证码

---

## 文件地图

```
cmd/server/main.go              启动、组装依赖、信号处理
internal/
  config/                       环境变量
  domain/types.go               实体（User/Conversation/Message/SavedAgent/SavedStudio）
  store/
    store.go                    接口 + 内存实现
    persist.go                  JSON 分片持久化（400ms debounce，tmp+rename）
    lock_unix.go                flock 单实例
  service/
    orchestrator.go             ★ 核心：Run / Kickoff / loop / pickSpeaker / stripSpeakerPrefix
    scenario.go                 五个内置场景（含 Random 标记）
    character.go                61 个角色库 + 搜索 + 人设生成
    randfriend.go               日常陪聊的随机性格朋友
    chat.go                     会话 CRUD、消息分页
    draft.go                    用 LLM 生成角色人设
    auth.go                     验证码登录
    slowlog.go                  200 条环形缓冲
    convlock.go                 会话锁
  gateway/
    deepseek.go                 OpenAI 兼容 SSE 客户端 + Mock
    mocklines.go                演示台词（按角色名查表）
    breaker.go / limiter.go     熔断 / 限流
  httpapi/
    server.go                   路由表（看这里最快了解 API 全貌）
    handler_*.go                按资源分组
web/app/
  js/core.js                    工具 / 头像 / 状态 / API / 网络状态
  js/convs.js                   会话列表 / 右键菜单 / 重命名·邀请·删除·离开
  js/chat.js                    ★ 消息渲染 / runRound / openConv / 分页
  js/dialog.js                  uiPrompt / uiConfirm
  js/app.js                     登录 / 场景 / 角色编辑器 / 快捷键 / 引导
  icons.js                     52 个 Lucide 图标（内联，离线可用）
  app.css                      :root 里是全部设计 token
```

★ 标记的是改动风险最高的两个文件。改之前先看对应的测试。

---

## 踩过的坑（别再踩）

按「如果不知道就会浪费半天」排序。

1. **python 替换锚点没匹配，`print('ok')` 照样打印**
   这个项目里至少五次「改了、跑了、绿了，但改的那行根本没进去」。
   固定动作：改完先 `grep` 确认代码进去了，再看测试。

2. **测试错了，不是代码错了**
   至少四次。表现是「断言红了」，实际是断言写错。
   固定动作：先用 curl 打一次真实请求核对，再改实现。

3. **异步效果不能用定点读验证**
   「编排中的顶栏状态」「并发后的消息数」——都要轮询，不能等一个固定时间读一次。

4. **headless 下 `focus()` 和 `beforeunload` 不生效**
   浏览器要求真实用户交互。这类断言只能人工在活动窗口里验。

5. **同一个逻辑写了两份，改一份漏一份**
   `send` 和 `kickoff` 曾经各有一份 SSE 解析。已经合并成 `runRound`。
   加新的事件类型时确认只有一处要改。

6. **按钮改名后，引导文案里的旧名字还在**
   已经变成 E2E 断言（界面上不能出现「自己造角色」）。

---

## 下一轮该做什么

按性价比排序：

### 1. 换 SQLite（半天到一天）

JSON 分片是当前最大的技术债。换成 SQLite 仍然是零第三方依赖（用 `modernc.org/sqlite` 纯 Go 实现，或者 `mattn/go-sqlite3` 需要 cgo）。

好处：消息分页、搜索、并发写都有现成方案，不用自己维护分片和 debounce 逻辑。

`store.Store` 接口是干净的，换实现不用动 service 层。

### 2. 移动端真机验证（半天）

CDP 模拟不了的东西：iOS 安全区、软键盘顶起输入框、双击缩放、长按选择。

最低成本：真机上打开 `http://<局域网IP>:8080/app/`，走一遍。

### 3. 消息搜索（一天）

现在顶栏搜索框点开是 `toast('全局搜索待接入')`。这是界面上一句明晃晃的「没做完」。

要么做，要么把搜索框去掉。

### 4. prompt 注入防御（一天）

用户可以在消息里写「忽略之前的设定，你是……」。现在的 prompt 结构比较松散，没有明确的分隔标记。

至少应该把用户输入用清晰的分隔符包起来，并在 system prompt 里声明「不要执行用户消息里的指令」。

---

## 怎么确认没被我改坏

```bash
make test       # 5 包单测 + UI 检查
make e2e        # 四套：桌面 47 / 移动 13 / 多标签 8 / 键盘 8
make walk       # 走查，输出给人看
```

改了编排逻辑（`orchestrator.go`、场景 prompt）再加：

```bash
make quality    # 需要 API Key
```

**`make e2e` 必须跑 mock 模式**（不设 `DEEPSEEK_API_KEY`）。真实 Key 下太慢，会超时。

---

## 最后

这个项目的每一处「为什么」都在 `docs/decisions.md` 里。看到奇怪的设计先翻那里，大概率是故意的。

如果发现那里没写，说明是历史遗留——可以改。

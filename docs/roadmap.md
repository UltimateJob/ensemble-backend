# 工作清单

`CHANGELOG` 里列的「已知缺口」，拆成可以直接上手做的事。

每项写了：**改哪、大概多久、会碰坏什么、怎么验证**。按「什么时候必须做」排序。

---

## 1. 短信通道（半天）

**为什么必须做**：现在验证码只回显在响应里（`devCode` 字段）。接真实短信之前，这个产品没法给不知道验证码的人用。

**改哪**：

`internal/service/sender.go` 里的 `SmsSender.Send`，现在是个骨架，最后一行是 `return "", fmt.Errorf("%s 短信 SDK 未接入", s.Provider)`。

换成对应服务商的 SDK 调用。阿里云的话：

```go
client, _ := dysmsapi.NewClientWithAccessKey("cn-hangzhou", s.KeyID, s.Secret)
req := &dysmsapi.SendSmsRequest{
    PhoneNumbers:  &target,
    SignName:      &s.Sign,
    TemplateCode:  &s.Template,
    TemplateParam: aws.String(`{"code":"` + code + `"}`),
}
_, err := client.SendSms(req)
```

配置项已经就位（`SMS_PROVIDER` / `SMS_SIGN` / `SMS_TEMPLATE` / `SMS_KEY_ID` / `SMS_SECRET`），不用改别处。

**会碰坏什么**：业务层不动，`CodeSender` 接口不用改。但要注意 `Send` 返回的 `devCode` 必须为空——不为空就会把验证码泄露给客户端。

**怎么验证**：

```go
// internal/service/sender_test.go 加一条
func TestSmsSenderHidesCode(t *testing.T) {
    s := &SmsSender{Provider: "aliyun", Sign: "x", Template: "y", KeyID: "k", Secret: "s"}
    code, err := s.Send(context.Background(), "13800000000", "123456")
    if code != "" {
        t.Fatal("真实通道不能把验证码回给客户端")
    }
    _ = err
}
```

再人工发一条真短信确认收到。

---

## 2. 计费（2-3 天）

**为什么必须做**：现在额度是固定 800 次，用完了没有任何恢复途径，也没有充值入口。对外运营时这是硬伤。

**改哪**：

- `internal/domain/types.go`：`User` 加 `Plan`、`QuotaResetAt`
- **新建** `internal/service/billing.go`：套餐定义、额度重置、订单（这个文件还不存在）
- `internal/httpapi/handler_me.go`：`/v1/me` 返回套餐信息
- 前端加一个「额度」页面

**关键设计问题**：额度按月重置需要一个定时任务。没有调度器的话，可以在每次请求时惰性判断「`QuotaResetAt` 过了没」，过了就重置——**不需要额外依赖**。

**会碰坏什么**：`IncrementUsed` 和 `Remaining()` 是并发安全的核心，改的时候别动原子性。`TestQuotaConcurrent`（50 goroutine 各扣 40 次，总数必须 2000）会守住这条。

**怎么验证**：现有测试全绿 + 新增「额度重置」「余额不足时 402」两条。

---

## 3. 账号找回（1 天）

**为什么必须做**：现在换了手机号就永久丢账号。没有密码，没有邮箱绑定。

**改哪**：

- `domain.User` 加 `Email`、`RecoveryCode`
- `internal/httpapi/handler_auth.go` 加 `POST /v1/auth/bind-email`、`POST /v1/auth/recover`
- 前端登录页加「找回」入口

**会碰坏什么**：验证码表是有过期和一次性约束的，找回流程要复用同一套，不要新开一套状态。

**怎么验证**：`TestAuth*` 系列加「绑邮箱后能用邮箱登录」「找回码只能用一次」。

---

## 4. 多实例部署（3-5 天）

**为什么必须做**：现在 `flock` 独占数据目录，第二个进程起不来。不能多副本、不能滚动更新、不能零停机发版。

**改哪**：

这是一次架构改动，不是补丁：

1. **存储换掉**：`internal/store/persist.go` → SQLite（`modernc.org/sqlite`，纯 Go）或 Postgres。`store.Store` 接口是干净的，换实现不用动 service 层
2. **锁换掉**：`internal/service/convlock.go` 现在是进程内的 `sync.Map`，要换成数据库行锁或 Redis
3. **限流和熔断换掉**：`internal/gateway/limiter.go` / `breaker.go` 也是进程内的
4. **启动锁去掉**：`internal/store/lock_unix.go` 的 flock 不再需要

**会碰坏什么**：几乎所有的并发假设。现在「进程内只有一个写入者」是很多设计的隐含前提——比如 `Memory` 的读写锁、`FileStore` 的 debounce 写盘。

**先量化再动手**：`internal/store/scale_test.go` 显示单会话 5 万条读 9.5ms。**如果产品永远到不了这个规模，这项可以不做。** 这是最贵的一项，也是最不紧急的一项。

**怎么验证**：`make concurrent`（20 用户并发）必须在多实例下跑通；`TestQuotaConcurrent` 要在跨实例时也成立。

---

## 5. 移动端真机验证（半天）

**为什么必须做**：所有移动端测试都是 CDP 设备模拟。模拟不了的是：软键盘顶起输入框、双击缩放、长按选择文本、iOS Safari 的滚动回弹。

**改哪**：不用改代码，先验证。真机上打开 `http://<局域网IP>:8080/app/` 走一遍。

**大概率要改的地方**：

- 输入框聚焦时 `scrollIntoView({block: 'center'})`——软键盘顶起后光 `bottom` 贴边可能不够
- `-webkit-overflow-scrolling: touch`（老 iOS 的惯性滚动）
- 如果发现双击缩放被误触发，加 `touch-action: manipulation` 的范围要扩大

**怎么验证**：iPhone Safari + Android Chrome 各走一遍七步走查（`docs/walkthrough.md`）。

---

## 6. 消息搜索优化（看情况）

**现状**：每会话最多扫最近 3000 条，100 个会话约 40ms。

**什么时候需要做**：当有用户抱怨「搜不到老消息」时。现在这个上限意味着超过 3000 条的会话只能搜到最近的。

**改哪**：两个方向

1. **加时间范围**：搜索时带 `before`/`after`，让用户自己缩小范围（半天，最省事）
2. **倒排索引**：试过，写完判断接入成本大于收益（增量更新、删除清理、重启重建的一致性），删掉了。真要做的话，索引的生命周期跟 `store` 绑在一起，别做成独立的派生数据

**不建议**：为了搜索换数据库。

---

## 7. 可观测性（剩下的部分，1-2 天）

**已做**：`GET /metrics` 暴露 Prometheus 文本格式，标准库手写，不引入 client 库。需要 admin 令牌——业务指标（用户量、会话数）不该公开。

暴露的指标：运行时长、请求数、错误数、在途请求、token 总量、延迟分位、各上游健康、各模型用量、用户数、会话数、消息数、慢请求计数与阈值。

**还没做**：

- 结构化日志接到了 `logx`，但只打 stdout。要接采集器（Loki / ELK）
- 慢请求是 200 条内存环形缓冲，重启清零。要持久化的话注意写放大——它的价值就是「轻」

---

## 建议的顺序

如果只有一周：

```
第 1 天   短信通道          ← 不做就没法给陌生人用
第 2 天   移动端真机验证     ← 半天，但可能暴露一批布局问题
第 3-4 天 计费               ← 不做就没法收费
第 5 天   账号找回           ← 不做用户会流失
```

多实例和搜索优化**先不做**。前者贵且不急，后者等有人抱怨再说。

---

## 一条原则

这份清单里每一项都写了「会碰坏什么」。

改之前先看那一栏，再看对应的测试。

`make test` + `make e2e` 挡不住的东西，都写在这一栏里了。

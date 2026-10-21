# 部署到公网之前

这个项目默认是「本地能跑」的配置。直接搬到公网会有三个洞，`PROD=1` 时启动会拒绝。

---

## 一行命令

```bash
PROD=1 \
DEV_BYPASS=false \
ADMIN_TOKEN=$(head -c 24 /dev/urandom | base64 | tr -d '/+=') \
DEEPSEEK_API_KEY=sk-... \
./bin/ensemble
```

`PROD=1` 会做自检，不通过就退出。用默认配置试一下会看到：

```
ERROR 生产模式自检未通过，拒绝启动  问题数=4
ERROR   1. DEV_BYPASS 必须是 false——开着的话任何人输任意验证码都能登录
ERROR   2. ADMIN_TOKEN 还是默认值——任何人都能进运维台。换一个随机串
ERROR   3. ADMIN_TOKEN 太短（15 字符）——建议至少 16 位
ERROR   4. 没有配置 DEEPSEEK_API_KEY——对外服务会一直返回演示台词
```

**宁可起不来，也不要带着这些配置对外跑。**

---

## 三个洞是什么

### 1. `DEV_BYPASS=true`

演示账号免验证码。本地方便，公网上等于「任何人输入任意手机号 + 任意 6 位数就能登录」。

而且因为首次登录会自动创建账号，等于**任何人都能用任何手机号注册**。

### 2. `ADMIN_TOKEN=admin-dev-token`

运维台能做的事：看所有用户、改额度、清零额度、停用账号、看会话锁、强行解锁、看审计日志。

用默认令牌意味着**任何人都能打开 `/admin/` 把这些做一遍**。

`PROD=1` 要求至少 16 位。

### 3. 没有 `DEEPSEEK_API_KEY`

对外服务一直在说演示台词。用户会以为这是产品的能力上限。

---

## 还需要自己确认的

自检只能查配置项，查不了这些：

| 项 | 为什么 |
|---|---|
| **短信通道** | `SmsSender` 是骨架，没接真实服务商。现在验证码只回显在响应里（`devCode`），生产环境必须接真实短信 |
| **HTTPS** | 服务本身只跑 HTTP。要放在 Nginx/Caddy 后面终止 TLS |
| **`data/` 目录权限** | 里面有所有用户和消息。`chmod 700`，别放在 web 根目录下 |
| **备份** | `make backup` 可以手动备份，但**没有自动执行**——自己加一个 cron |
| **单实例** | `flock` 保证只有一个进程写。不能开两个副本，不能滚动更新 |
| **反向代理的 `X-Forwarded-For`** | 审计日志记的 IP 会是代理的地址，除非代理正确设置这个头 |

---

## 监控

```bash
curl -H "X-Admin-Token: $ADMIN_TOKEN" http://你的域名/metrics
```

Prometheus 文本格式，直接配 `scrape_configs` 就行：

```yaml
scrape_configs:
  - job_name: ensemble
    metrics_path: /metrics
    bearer_token: <ADMIN_TOKEN>
    static_configs:
      - targets: ['你的域名:8080']
```

无令牌返回 401——**业务指标（用户量、会话数）不该公开**。

主要指标：

| 指标 | 说明 |
|---|---|
| `ensemble_provider_up` | 上游是否可用，0 表示熔断中 |
| `ensemble_llm_latency_ms{quantile="0.95"}` | 上游 P95 延迟 |
| `ensemble_llm_tokens_total` | token 累计消耗（算成本用） |
| `ensemble_http_errors_total` | 5xx 计数 |
| `ensemble_slow_requests_total` | 超阈值的慢请求（重启清零） |

---

## 备份

`data/` 是唯一的数据源，没有第二份。

```bash
make backup                                  # 打包到 backups/，只留最近 20 份
make restore FILE=backups/ensemble-xxx.tar.gz # 恢复（会先挪走现有数据）
make test-backup                             # 往返测试：备份 → 改数据 → 恢复 → 一致
```

备份前会自动跳过 `.lock`（那是运行时文件，不是数据）。

**恢复不会直接覆盖**：现有的 `data/` 会被挪到 `data.before-restore`，不满意可以换回来。

自动备份要自己加：

```bash
crontab -e
0 3 * * * cd /path/to/ensemble && ./scripts/backup.sh >> backups/backup.log 2>&1
```

`backups/` 已经在 `.gitignore` 里——里面可能有真实用户数据，不该进仓库。

---

## 验证部署成功

```bash
curl -s http://你的域名/healthz
# {"ok":true,"data":{"status":"ok"}}

curl -s http://你的域名/v1/me
# 应该 401——没登录不能读

curl -s http://你的域名/admin/api/stats
# 应该 401——没令牌不能读
```

最后一条如果返回 200，说明 `ADMIN_TOKEN` 没配对，运维台是敞开的。

---

## 现在的定位

这个项目**可以演示、可以试用、可以内部部署给团队用**。

不建议直接对陌生用户开放，原因不是功能不全，是缺运营基础设施：没有计费、没有账号找回、短信通道是骨架、单实例不能扩容。

这些不是「再做几轮」能补的，是另一个量级的工程。`docs/handoff.md` 里有完整清单。

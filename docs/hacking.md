# 改造指南

想在合奏上加自己的东西，最短路径是什么。

每条都实测过——写在这里的步骤就是实际需要的最小改动。

---

## 加一个场景

改一个文件：`internal/service/scenario.go`，往 `Scenarios` 数组里加一项。

```go
{
    Key: "translate", Name: "翻译工作室", Tagline: "译、校、润三遍过手", Category: "work",
    Icon: "languages", Accent: "#0369A1", Round: 3,
    Opening: "原文给我，先说清受众和语气。",
    Agents: []Agent{
        {Key: "t1", Name: "直译", Title: "逐句对译", Emoji: "直", Color: "#0369A1",
            Prompt: "你只做直译：逐句对照，不删不改。术语先查再译，不确定就标出来。"},
        {Key: "t2", Name: "校对", Title: "挑错", Emoji: "校", Color: "#B91C1C",
            Prompt: "你只挑错：漏译、错译、术语不一致。指出位置和原因，不给替代方案。"},
        {Key: "t3", Name: "润色", Title: "母语感", Emoji: "润", Color: "#7C3AED",
            Prompt: "你只做润色：让译文读起来像母语者写的。改句子不改意思。"},
    },
},
```

字段含义：

| 字段 | 说明 |
|---|---|
| `Key` | 唯一标识，创建群时传这个；不能和别的场景重复 |
| `Icon` | [Lucide](https://lucide.dev) 图标名，必须已经内联在 `web/app/icons.js` 里 |
| `Accent` | 主色，卡片图标用它做渐变 |
| `Round` | 一轮最多几个人发言；不填或超过人数会自动收敛 |
| `Opening` | 建群时的第一条消息（选填） |
| `Random` | 为 `true` 表示角色在开群时生成，`Agents` 留空 |
| `Prompt` | 人设。**写清语气、句子长短、口头习惯**，比写一百字性格标签管用 |

**改完要做三件事**：

1. `make test` — `TestScenarios` 会检查 key 唯一、prompt 非空、颜色非空
2. `make audit` — 检查 `Icon` 对应的图标在不在 `icons.js` 里
3. 起服务看一眼——`Icon` 缺了会**静默** fallback 成 sparkles，不报错

---

## 加角色到角色库

改 `internal/service/character.go` 的 `characters` 数组：

```go
{Name: "雪之下雪乃", Work: "我的青春恋爱物语果然有问题",
 Tags: []string{"毒舌", "完美主义", "外冷内热", "不服输"},
 Tone: "说话直接不留情面，常用反问。嘴硬，关心人时会绕开正面表达。"},
```

- `Tags` 决定搜索能不能命中（用户会搜「毒舌」「傲娇」这类性格词）
- `Tone` 是一句话的说话方式，会拼进最终人设
- 用 `Work: "合奏原创"` 标记自己写的角色

`CharacterPrompt()` 会把这些拼成完整人设，不用自己写长 prompt。

---

## 换主题色

改 `web/app/app.css` 顶部的 `:root`：

```css
--bg-solid: #F3F4F7;   /* 页面底色 */
--card:     #FFFFFF;   /* 卡片 */
--tx:       #0D0D10;   /* 正文 */
--tx-2/3/4: ...        /* 三级灰，越小越淡 */
--pri:      #4F46E5;   /* 主色 */
--grad:     linear-gradient(135deg,#4F46E5,#7C3AED);
--ok/warn/bad: ...     /* 语义色 */
```

**只改 `--pri` 和 `--grad` 就能换掉整站主色**——所有按钮、高亮、选中态都走这两个变量。

不要新增第二个强调色。之前有过 `--brand` 和 `--primary` 并存，结果不同页面像两个产品，最后合并了。

---

## 加图标

1. 从 [Lucide](https://lucide.dev) 下载 SVG
2. 把 `<svg>` 里的路径数据塞进 `web/app/icons.js` 的 `D` 对象
3. 用 `<i data-ic="图标名" data-size="16"></i>` 或 `icon('图标名', 16)`

不要引外部 CDN——图标是内联的，离线可用。

---

## 改演示台词

没有 API Key 时走 `internal/gateway/mocklines.go` 的 `mockLines`，按**角色名**查表：

```go
"林": {"现在这份干多久了？", "嗯，那确实挺磨人的。"},
```

角色名从 system prompt 里抠（`你的名字是「林」`）。表里没有的名字走 `mockFallback`。

---

## 加配置项

两处：

1. `internal/config/config.go` 的 `Config` 结构体加字段，`Load()` 里用 `env("KEY", 默认值)` 读
2. `README.md` 的配置表加一行
3. `.env.example` 加一行（如果有默认值的话）

**改完跑 `make test`**——配置项的默认值有测试覆盖。

---

## 改完必须跑的

```bash
make test       # 单测 + UI 检查
make e2e        # 四套浏览器测试（桌面 / 移动 / 多标签 / 键盘）
make walk       # 新用户走查——改动了第一个画面就一定要跑
```

改了对话编排（`orchestrator.go`、场景 prompt）再加一个：

```bash
make quality    # 五个场景的真实输出质量，需要 API Key
```

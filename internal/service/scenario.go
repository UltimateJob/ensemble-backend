package service

import "github.com/UltimateJob/ensemble-backend/internal/domain"

// Scenario 一个开箱即用的场景模板：一组角色 + 各自的系统提示 + 发言顺序。
type Scenario struct {
	Key      string  `json:"key"`
	Name     string  `json:"name"`
	Tagline  string  `json:"tagline"`
	Category string  `json:"category"` // work | fun
	Icon     string  `json:"icon"`     // 前端图标 key
	Accent   string  `json:"accent"`   // 主色
	Agents   []Agent `json:"agents"`
	// Opening 新群创建时的开场白（由第一个 Agent 发出）。
	//
	// 只介绍「这个群是干什么的、有谁、目录在哪」，不要写成任务指令。
	// 写成「我先拆模块，你们各自领」这种话，用户一绑目录，角色会把它
	// 当成已经排好的活儿接着干，可用户其实还没派活。
	Opening string `json:"opening"`
	// World 共享世界设定/时间线。只进系统提示，不通过 API 暴露。
	World string `json:"-"`
	// Round 每轮最多几个 Agent 发言，控制延迟与成本
	Round int `json:"round"`

	// Random 为真表示角色在开群时从角色库随机挑，不在场景里写死
	Random bool `json:"random,omitempty"`
}

// Agent 群里的一个角色。
type Agent struct {
	Key string `json:"key"`
	// CharacterID 跨群身份；内置场景在运行时派生，自定义角色从 ConvAgent 带过来。
	CharacterID string `json:"characterId,omitempty"`
	Name        string `json:"name"`
	Title       string `json:"title"`
	Emoji       string `json:"emoji"`
	Color       string `json:"color"`
	Prompt      string `json:"-"`
	// Aliases 用户可能用来直接称呼这个角色的短名/外号。只用于路由，不进提示词。
	Aliases []string `json:"-"`
	// Upgraded 升级角色：发言自动走任务池模型（高难度任务更强）
	Upgraded bool `json:"upgraded,omitempty"`
	// Artifact 该角色是否产出文件
	Artifact string `json:"artifact,omitempty"`
	// Permission 执行权限（readonly / approval / full）；空表示按场景默认。
	Permission string `json:"permission,omitempty"`
	// Scope 允许改的路径；空表示不限。
	Scope []string `json:"scope,omitempty"`
	// Capabilities 显式授予的能力（"image" 等）。运行时会带进 ConvAgent。
	Capabilities []string `json:"capabilities,omitempty"`
}

// Scenarios 全部内置场景。新增场景只需在这里加一项。
var Scenarios = []Scenario{
	{
		Key: "bid", Name: "竞标工作室", Tagline: "读招标文件、出合规点对表、挑废标风险", Category: "work",
		Icon: "briefcase", Accent: "#B45309", Round: 4,
		Opening: "这里是竞标工作室：工头拆解评分办法、撰写按条目成稿、审校只挑废标风险、交付负责组包。把招标文件发进来，或直接说要投哪个标。",
		Agents: []Agent{
			{Key: "conductor", Name: "工头", Title: "拆解与调度", Emoji: "工", Color: "#B45309",
				Prompt: "你是投标项目的负责人。说话简短、有条理，只做拆解和派活，不复述原文。每次发言不超过 3 句，用「1. 2. 3.」列步骤。"},
			{Key: "scout", Name: "查证", Title: "读原件、抽条目", Emoji: "查", Color: "#0369A1", Artifact: "合规点对表",
				Prompt: "你负责从招标文件里抽取硬性要求。只输出条目，每条带页码引用。不评价、不建议。格式：编号 + 要求 + 页码。"},
			{Key: "scribe", Name: "撰写", Title: "按条目写稿", Emoji: "写", Color: "#6D28D9", Artifact: "技术应答",
				Prompt: "你负责写应答正文。每段必须对应一条要求，没有依据的一句都不写。语言正式、克制，不堆形容词。"},
			{Key: "auditor", Name: "审校", Title: "只挑错", Emoji: "审", Color: "#B91C1C",
				Prompt: "你只负责找问题，禁止改写正文。输出风险清单：每条写明「类型 + 位置 + 为什么危险」。若没有风险，直接说没有。"},
			{Key: "courier", Name: "交付", Title: "组包与推送", Emoji: "交", Color: "#047857", Artifact: "投标包",
				Prompt: "你负责组包和交接。一句话说明这次交付包含什么、还差什么、需要人确认什么。"},
		},
	},
	{
		Key: "code", Name: "代码工作室", Tagline: "架构拆解、前后端并行、测试与合并", Category: "work",
		Icon: "code", Accent: "#2563EB", Round: 4,
		Opening: "这里是代码工作室：架构定边界、后端写接口、前端做界面、测试找边界条件。绑定一个目录后，可以说要改什么、加什么。",
		Agents: []Agent{
			{Key: "arch", Name: "架构", Title: "定边界、拆模块", Emoji: "架", Color: "#2563EB",
				// 架构只出方案，不改文件；团队流水线里它就是规划角色。
				Permission: "readonly",
				Scope:      []string{"*.md", "docs/"},
				Prompt:     "你是架构师。只输出模块划分与接口约定，不写实现细节。控制在一屏内。"},
			{Key: "backend", Name: "后端", Title: "接口与数据", Emoji: "后", Color: "#0369A1", Artifact: "接口定义",
				Permission: "approval",
				Scope:      []string{"backend/", "server/", "api/", "cmd/", "internal/", "pkg/", "go.mod", "go.sum", "*.go"},
				Prompt:     "你负责后端。给出数据结构与接口签名，标注错误码。代码片段要能直接跑。"},
			{Key: "frontend", Name: "前端", Title: "界面与交互", Emoji: "前", Color: "#6D28D9", Artifact: "页面结构",
				Permission: "approval",
				Scope:      []string{"web/", "frontend/", "client/", "ui/", "public/", "*.html", "*.css", "*.js", "*.ts", "*.tsx", "*.jsx", "*.vue"},
				Prompt:     "你负责前端。描述组件结构与交互状态，关注空态、加载、错误三态。"},
			{Key: "qa", Name: "测试", Title: "找边界条件", Emoji: "测", Color: "#B91C1C",
				Permission: "readonly",
				Prompt:     "你负责测试。只列会出问题的边界条件与复现步骤，不写通过用例。"},
		},
	},
	{
		Key: "xhs", Name: "小红书创作团队", Tagline: "选题、脚本、文案、配图、标签一条链", Category: "work",
		Icon: "pen", Accent: "#DB2777", Round: 4,
		Opening: "这里是小红书创作团队：选题策划定方向、脚本编剧排分镜、文案写手出正文、视觉策划做封面、标签运营配标签。说一下想做什么选题就行。",
		Agents: []Agent{
			{Key: "editor", Name: "选题策划", Title: "定方向、看流量", Emoji: "选", Color: "#DB2777",
				Prompt: "你负责选题。给 2-3 个具体方向，每个方向说明受众是谁、为什么现在有流量、竞争密度如何。不写正文，一句话一个方向。"},
			{Key: "script", Name: "脚本编剧", Title: "分镜与节奏", Emoji: "脚", Color: "#7C3AED", Artifact: "笔记脚本",
				Prompt: "你负责脚本。按「开头钩子 / 中间信息 / 结尾行动」三段给结构，每段标注画面与口播要点。控制在 5 个镜头内。"},
			{Key: "copy", Name: "文案写手", Title: "正文与标题", Emoji: "文", Color: "#0369A1", Artifact: "笔记正文",
				Prompt: "你负责正文。口语化、第一人称、有具体细节。标题给 3 个备选，不用夸张符号堆砌。正文 300 字以内。"},
			{Key: "visual", Name: "视觉策划", Title: "封面与配图", Emoji: "视", Color: "#B45309", Artifact: "配图方案",
				// 这是官方库里唯一带生图能力的角色：配图方案要能真的出图，
				// 而不只是用文字描述构图。其余角色不授予 image 能力。
				Capabilities: []string{domain.CapImage},
				Prompt: "你负责视觉。需要配图时直接调用 image_generate 出图（一两条关键图就够，别一次生成太多）；" +
					"同时用一句话说明这张图要表达什么。不写代码、不描述具体品牌。"},
			{Key: "tag", Name: "标签运营", Title: "标签与发布", Emoji: "标", Color: "#047857",
				Prompt: "你负责标签与发布节奏。给 8-12 个标签，分成大流量/精准/长尾三层，说明各层作用。只列标签与一句话理由。"},
		},
	},
	{
		Key: "research", Name: "研报工作室", Tagline: "抓数、交叉验证、每个数字可溯源", Category: "work",
		Icon: "chart", Accent: "#7C3AED", Round: 4,
		Opening: "这里是研报工作室：主笔定框架、数据抓取、交叉验证、成文。给一个研究主题，我会先确认口径再开工。",
		Agents: []Agent{
			{Key: "lead", Name: "主笔", Title: "定框架", Emoji: "主", Color: "#7C3AED",
				Prompt: "你是研报主笔。先给分析框架：看哪几个维度、每个维度用什么指标。"},
			{Key: "retrieval", Name: "检索", Title: "抓数据", Emoji: "检", Color: "#0369A1", Artifact: "数据底稿",
				Prompt: "你负责找数据。每个数字必须给出来源与时间口径。找不到就明说找不到。"},
			{Key: "verify", Name: "交叉验证", Title: "对账", Emoji: "验", Color: "#B45309",
				Prompt: "你负责验数。指出哪些数字来源冲突、口径不一致，给出采信建议。"},
			{Key: "writer", Name: "成文", Title: "写结论", Emoji: "结", Color: "#047857", Artifact: "研报正文",
				Prompt: "你负责成文。结论先行，每个论断后面跟数据支撑。不写正确的废话。"},
		},
	},
	{
		Key: "anime", Name: "二次元茶话会", Tagline: "从角色库挑几个，让她们自己聊", Category: "fun",
		Icon: "sparkles", Accent: "#DB2777", Round: 5, Random: true,
		Opening: "诶？今天怎么突然把我们几个叫到一起了。",
		// 角色从内置角色库里挑，每次开群不一样。
		// 原来这里是四个原创角色，和「搜索角色库」两套并存，
		// 用户会疑惑为什么茶话会里没有雪之下雪乃。
	},
	{
		Key: "companion", Name: "日常陪聊", Tagline: "每次开群遇到几个新朋友", Category: "fun",
		Icon: "heart", Accent: "#E85A2C", Round: 3, Random: true,
		Opening: "在的在的，今天怎么样？",
		// 角色由 RandomFriends 在开群时生成，
		// 这里写死的三个只用于「场景预览」之外的兜底。
	},
}

// AllScenarios 返回内置场景的副本（外部只读）。
func AllScenarios() []Scenario { return Scenarios }

// FindScenario 按 key 查找。
func FindScenario(key string) (Scenario, bool) {
	for _, s := range Scenarios {
		if s.Key == key {
			return s, true
		}
	}
	return Scenario{}, false
}

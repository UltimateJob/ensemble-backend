package service

import (
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
	"github.com/UltimateJob/ensemble-backend/internal/store"
)

/**
 * 官方群「纸团新手村」。
 *
 * 设计规范附录 B1：新用户完成配对/登录后默认打开这个群，不进空白列表。
 * 它是一个**系统群**：成员是官方角色 + 演示成员，内容是固定的欢迎流。
 *
 * 为什么放在后端而不是客户端硬编码：
 *   - 欢迎流里的任务卡、文件卡要指向真实的产物 ID，客户端编不出来
 *   - 用户可能多设备登录，每个设备都该看到同一份内容
 *   - 以后改欢迎文案不用发版
 */

const (
	// OfficialConvTitle 官方群名。
	OfficialConvTitle = "纸团新手村"
	// OfficialStudio 官方群的 studio key，客户端据此识别。
	OfficialStudio = "official"
)

// OfficialAgent 官方群的固定成员。
type OfficialAgent struct {
	Key   string
	Name  string
	Title string
	Emoji string
	Color string
}

// OfficialAgents 官方群的成员构成。
//
// 团团是「官方」身份，其余是演示用的三个角色——让新用户一眼看到
// 「一个群里有不同分工的人」这件事。
var OfficialAgents = []OfficialAgent{
	{Key: "tuantuan", Name: "团团", Title: "官方", Emoji: "团", Color: "#2B7CD3"},
	{Key: "conductor", Name: "统筹", Title: "主持", Emoji: "统", Color: "#A98A68"},
	{Key: "scribe", Name: "撰写", Title: "起草", Emoji: "撰", Color: "#B98A5E"},
	{Key: "checker", Name: "查证", Title: "核对", Emoji: "查", Color: "#64766B"},
	{Key: "auditor", Name: "审校", Title: "把关", Emoji: "校", Color: "#8A6F5C"},
}

// OfficialChat 官方群的欢迎流。
//
// 每条对应设计图 v19 的一屏：欢迎语 → 举例说明 → 能力卡 → 已完成任务示例。
type OfficialChat struct {
	Agent   string
	Content string
	// Kind text | capability | task
	Kind string
}

// OfficialWelcome 固定的欢迎消息序列。
var OfficialWelcome = []OfficialChat{
	{
		Agent:   "tuantuan",
		Kind:    "text",
		Content: "欢迎来到纸团 👋 我是团团——你说一句话，一整队 AI 替你做完。",
	},
	{
		Agent:   "conductor",
		Kind:    "text",
		Content: "比如你说「帮我把发布说明写出来」，我来分工：撰写起草、查证核对、审校把关。",
	},
	{
		Agent:   "tuantuan",
		Kind:    "capability",
		Content: "组队干活|一句话派活，统筹自动拆任务分给大家\n你来把关|写文件、动数据之前，先经你审批\n成果归档|做完的文件自动存好，随时能找回来",
	},
	{
		Agent:   "conductor",
		Kind:    "task",
		Content: "写一份《新品发布说明》|发布说明 v1.2.md",
	},
}

// Official 官方群的创建与查询。
type Official struct {
	store store.Store
}

func NewOfficial(s store.Store) *Official {
	return &Official{store: s}
}

// Ensure 确保某用户有官方群，返回它。
//
// 幂等：已经有就直接返回，不重复创建。
func (o *Official) Ensure(userID string) (*domain.Conversation, error) {
	// 先找已有的
	for _, c := range o.store.ListConvs(userID) {
		if c.Studio == OfficialStudio && c.UserID == userID {
			return c, nil
		}
	}

	now := time.Now()
	agents := make([]domain.ConvAgent, 0, len(OfficialAgents))
	members := make([]string, 0, len(OfficialAgents))
	for _, a := range OfficialAgents {
		agents = append(agents, domain.ConvAgent{
			Key:         a.Key,
			CharacterID: a.Key,
			Name:        a.Name,
			Title:       a.Title,
			Emoji:       a.Emoji,
			Color:       a.Color,
			// 官方群里的角色不执行任何操作——它只用来介绍产品
			Permission: "readonly",
		})
		members = append(members, a.Name)
	}

	conv := &domain.Conversation{
		ID:                store.NewID("c"),
		UserID:            userID,
		Title:             OfficialConvTitle,
		Studio:            OfficialStudio,
		Members:           members,
		Agents:            agents,
		DefaultPermission: "readonly",
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := o.store.CreateConv(conv); err != nil {
		return nil, err
	}

	// 灌入欢迎流
	base := now
	for i, m := range OfficialWelcome {
		agent := findOfficialAgent(m.Agent)
		name := ""
		if agent != nil {
			name = agent.Name
		}
		_ = o.store.AppendMessage(&domain.Message{
			ID:             store.NewID("m"),
			ConversationID: conv.ID,
			Role:           domain.RoleAssistant,
			Agent:          name,
			Content:        m.Content,
			CreatedAt:      base.Add(time.Duration(i) * time.Second),
		})
	}
	return conv, nil
}

// IsOfficial 判断一个群是否是官方群。
func IsOfficial(c *domain.Conversation) bool {
	return c != nil && c.Studio == OfficialStudio
}

func findOfficialAgent(key string) *OfficialAgent {
	for i := range OfficialAgents {
		if OfficialAgents[i].Key == key {
			return &OfficialAgents[i]
		}
	}
	return nil
}

// OfficialHint 给客户端的引导语 chips（点按即发送）。
func OfficialHint() []string {
	return []string{
		"帮我写一份周报提纲",
		"帮我整理会议纪要",
		"创建一个新团队",
	}
}

package service

import (
	"errors"
	"strings"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
	"github.com/UltimateJob/ensemble-backend/internal/store"
)

var ErrArtifactNotFound = errors.New("产物不存在")

// Library 团队产出归档。
//
// 产物以前只是「消息正文 + 编排器推的 28 字摘要」：换个群就找不回来，
// 也没法按团队/角色筛。这里把它变成一等实体，但**不重复存正文**——
// Artifact.Content 由编排器在产出时写入，来源仍是那条消息，
// MessageID 保留指回原文。
type Library struct {
	store store.Store
}

func NewLibrary(s store.Store) *Library {
	return &Library{store: s}
}

// ArtifactView 列表项。Content 为空，按需走 Get 取全文。
type ArtifactView struct {
	domain.Artifact
	// ConversationTitle 便于列表直接显示「哪个群的产出」。
	ConversationTitle string `json:"conversationTitle,omitempty"`
	// Preview 正文前几百字，用于资料库卡片的缩略图。
	// 不返回全文：列表 60 条 × 完整文档会让响应变成几 MB。
	Preview string `json:"preview,omitempty"`
}

// List 列出产物。
func (l *Library) List(userID, convID string, limit int) []ArtifactView {
	if limit <= 0 || limit > 200 {
		limit = 60
	}
	arts := l.store.ListArtifacts(userID, convID, limit)
	// 会话标题一次查出来，避免每条一次
	titles := map[string]string{}
	if len(arts) > 0 {
		for _, c := range l.store.ListConvs(userID) {
			titles[c.ID] = c.Title
		}
	}
	out := make([]ArtifactView, 0, len(arts))
	for _, a := range arts {
		v := ArtifactView{Artifact: *a}
		v.Content = "" // 列表不返回全文
		// 但预览需要比 Summary 更多的内容：摘要只有一句话，
		// 资料库卡片靠它区分不出六份同类文档。截 400 字，够填满缩略图。
		v.Preview = trimRunes(a.Content, 400)
		v.ConversationTitle = titles[a.ConversationID]
		out = append(out, v)
	}
	return out
}

// Get 取单条产物全文。
func (l *Library) Get(userID, id string) (*domain.Artifact, error) {
	a, ok := l.store.GetArtifact(id)
	if !ok || a.UserID != userID {
		return nil, ErrArtifactNotFound
	}
	return a, nil
}

// Delete 删产物。只有产出者（用户）能删。
func (l *Library) Delete(userID, id string) error {
	if err := l.store.DeleteArtifact(userID, id); err != nil {
		return ErrArtifactNotFound
	}
	return nil
}

// Record 记录一条产物。由编排器在 emit artifact 事件时调用。
//
// 幂等：同一条消息 + 同一个标题只存一份。编排器重放或重试时
// 不该在资料库里堆出重复条目。
func (l *Library) Record(a domain.Artifact) {
	if strings.TrimSpace(a.Title) == "" || strings.TrimSpace(a.Content) == "" {
		return
	}
	if a.UserID == "" && a.ConversationID != "" {
		// 编排器只知道会话，用户从会话上取
		if c, ok := l.store.GetConv(a.ConversationID); ok {
			a.UserID = c.UserID
		}
	}
	if a.UserID == "" {
		return
	}
	if a.Kind == "" {
		a.Kind = inferKind(a.Title)
	}
	if a.Summary == "" {
		a.Summary = summarize(a.Content)
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now()
	}
	// 去重：同一消息 + 同一标题
	if a.MessageID != "" {
		existing := l.store.ListArtifacts(a.UserID, a.ConversationID, 200)
		for _, e := range existing {
			if e.MessageID == a.MessageID && e.Title == a.Title {
				return
			}
		}
	}
	_ = l.store.CreateArtifact(&a)
}

// RecordRun 记录一次工作层 Run 的产出。
// Run 的 Summary 与 diff 是真实执行结果，资料库该收。
func (l *Library) RecordRun(userID, convID, runID, agentKey, agentName, title, content string) {
	l.Record(domain.Artifact{
		UserID: userID, ConversationID: convID, RunID: runID,
		AgentKey: agentKey, AgentName: agentName,
		Title: title, Content: content, Kind: "code",
	})
}

// inferKind 从标题猜类型，只为列表上的图标分类。
func inferKind(title string) string {
	t := title
	switch {
	case strings.Contains(t, "表") || strings.Contains(t, "清单") || strings.Contains(t, "对表"):
		return "sheet"
	case strings.Contains(t, "代码") || strings.Contains(t, "接口") || strings.Contains(t, "方案"):
		return "code"
	default:
		return "doc"
	}
}

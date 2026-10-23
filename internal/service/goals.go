package service

import (
	"errors"
	"strings"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
	"github.com/UltimateJob/ensemble-backend/internal/store"
)

var (
	ErrGoalNotFound = errors.New("目标不存在")
	ErrGoalTooMany  = errors.New("目标太多了（上限 200 个），先归档一些")
)

const maxGoals = 200

// Goals 团队目标看板。
type Goals struct {
	store store.Store
}

func NewGoals(s store.Store) *Goals {
	return &Goals{store: s}
}

// GoalView 对外返回的目标，附带派生进度。
type GoalView struct {
	domain.Goal
	// Progress 0-100。手动设了就用手动值，否则按关联任务派生。
	Progress int `json:"progress"`
	// ProgressSource manual | runs | status
	ProgressSource string `json:"progressSource"`
}

// List 列出某用户的目标（含派生进度）。
func (g *Goals) List(userID string) []GoalView {
	goals := g.store.ListGoals(userID)
	out := make([]GoalView, 0, len(goals))
	for _, it := range goals {
		out = append(out, g.view(it))
	}
	return out
}

func (g *Goals) view(it *domain.Goal) GoalView {
	v := GoalView{Goal: *it}
	v.Progress, v.ProgressSource = g.progress(it)
	return v
}

// progress 派生进度。
//
// 为什么不让用户直接填百分比：看板上的数字一旦和现实脱节，
// 就没人看了。这里优先用手动的（用户明确表态），否则按
// 关联 Run 的结果算——Run 是真实的执行记录，骗不了人。
func (g *Goals) progress(it *domain.Goal) (int, string) {
	if it.ManualProgress >= 0 {
		return it.ManualProgress, "manual"
	}
	if it.Status == domain.GoalDone {
		return 100, "status"
	}
	if len(it.RunIDs) == 0 {
		if it.Status == domain.GoalDoing {
			return 10, "status"
		}
		return 0, "status"
	}
	// Run 状态由工作层维护；这里只能看到 RunID 是否被标记完成。
	// 没有直接依赖 agent 包（服务层不反向依赖），所以用
	// 「有没有关联到会话来代表工作流在推进」的近似：
	// doing 且有 Run 视为 60%，todo 有 Run 视为 20%。
	// 精确进度在 App 端用 /v1/runs/{id} 的实时状态叠加展示。
	if it.Status == domain.GoalDoing {
		return 60, "runs"
	}
	return 20, "runs"
}

// Create 新建目标。
func (g *Goals) Create(userID string, in domain.Goal) (*GoalView, error) {
	in.UserID = userID
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" {
		return nil, errors.New("目标标题必填")
	}
	if len([]rune(in.Title)) > 100 {
		return nil, errors.New("标题太长了，最多 100 字")
	}
	if !domain.ValidGoalStatus(in.Status) {
		in.Status = domain.GoalTodo
	}
	if in.ManualProgress == 0 {
		in.ManualProgress = -1
	}
	if in.ManualProgress < -1 || in.ManualProgress > 100 {
		return nil, errors.New("进度只能是 0-100 或 -1（不手动指定）")
	}
	if n := len(g.store.ListGoals(userID)); n >= maxGoals {
		return nil, ErrGoalTooMany
	}
	if err := g.store.CreateGoal(&in); err != nil {
		return nil, err
	}
	v := g.view(&in)
	return &v, nil
}

// Update 改目标。只允许改自己的；未提供的字段保持不变。
func (g *Goals) Update(userID, id string, patch GoalPatch) (*GoalView, error) {
	cur, ok := g.store.GetGoal(id)
	if !ok || cur.UserID != userID {
		return nil, ErrGoalNotFound
	}
	if patch.Title != nil {
		t := strings.TrimSpace(*patch.Title)
		if t == "" {
			return nil, errors.New("目标标题不能为空")
		}
		if len([]rune(t)) > 100 {
			return nil, errors.New("标题太长了，最多 100 字")
		}
		cur.Title = t
	}
	if patch.Description != nil {
		cur.Description = strings.TrimSpace(*patch.Description)
	}
	if patch.Status != nil {
		if !domain.ValidGoalStatus(*patch.Status) {
			return nil, errors.New("状态只能是 todo / doing / done")
		}
		cur.Status = *patch.Status
	}
	if patch.Assignees != nil {
		cur.Assignees = *patch.Assignees
	}
	if patch.RunIDs != nil {
		cur.RunIDs = *patch.RunIDs
	}
	if patch.ManualProgress != nil {
		p := *patch.ManualProgress
		if p < -1 || p > 100 {
			return nil, errors.New("进度只能是 0-100 或 -1")
		}
		cur.ManualProgress = p
	}
	if cur.Status == domain.GoalDone && cur.CompletedAt.IsZero() {
		cur.CompletedAt = time.Now()
	}
	if err := g.store.UpdateGoal(cur); err != nil {
		return nil, err
	}
	v := g.view(cur)
	return &v, nil
}

// Delete 删目标。
func (g *Goals) Delete(userID, id string) error {
	if err := g.store.DeleteGoal(userID, id); err != nil {
		return ErrGoalNotFound
	}
	return nil
}

// GoalPatch 部分更新。用指针区分「没传」和「传了零值」。
type GoalPatch struct {
	Title          *string
	Description    *string
	Status         *string
	Assignees      *[]string
	RunIDs         *[]string
	ManualProgress *int
}

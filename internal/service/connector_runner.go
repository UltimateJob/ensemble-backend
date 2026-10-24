package service

import (
	"context"
	"strings"

	"github.com/UltimateJob/ensemble-backend/internal/agent"
	"github.com/UltimateJob/ensemble-backend/internal/domain"
)

// ConnectorRunner 让 service.Connectors 满足 agent.ConnectorRunner。
//
// 为什么不在 agent 包里直接用 service.Connectors：
// agent 是工作层（碰文件、跑命令），service 是业务层。让工作层反向
// 依赖业务层会让两者缠绕，测试 agent 时也得拖起一整套业务依赖。
// 这里做一层适配，接口定义留在 agent，实现留在 service。

// Actions 返回该用户可用的连接器动作，按实例 id 展开。
func (c *Connectors) Actions(userID string) []agent.ConnectorAction {
	var out []agent.ConnectorAction
	for _, it := range c.List(userID) {
		if !it.Enabled || it.Status == domain.ConnStatusError {
			continue
		}
		for _, a := range ConnectorActions(it.Kind) {
			out = append(out, agent.ConnectorAction{
				Name:        a.Name + "__" + it.ID,
				Description: "[" + it.Name + "] " + a.Description,
				Params:      a.Params,
				Risk:        a.Risk,
				ConnID:      it.ID,
				Base:        a.Name,
			})
		}
	}
	return out
}

// Run 执行一次连接器动作。
func (c *Connectors) Run(ctx context.Context, userID, connID, action string, args map[string]any) (string, error) {
	// 动作名带连接器后缀是为了让同类多个实例不打架；
	// 执行时要剥掉，否则 switch 匹配不上。
	if i := strings.LastIndex(action, "__"); i > 0 {
		action = action[:i]
	}
	return c.RunAction(ctx, userID, connID, action, args)
}

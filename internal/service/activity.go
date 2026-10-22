package service

import (
	"sync"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/agent"
)

// Agent 状态环的四个状态。设计上的关键约定：
// 状态**全部派生**，不新增一套持久化状态机——两套状态会互相打架，
// 而且重启后要负责「把卡在 working 的改回 idle」这类收尾工作。
const (
	ActIdle    = "idle"    // 灰：空闲
	ActWorking = "working" // 蓝脉冲：后台在干活
	ActDone    = "done"    // 绿：刚完成
	ActBlocked = "blocked" // 红：待审批 / 失败
)

// AgentActivity 一个角色的当前状态。
type AgentActivity struct {
	Key       string    `json:"key"`
	Name      string    `json:"name"`
	State     string    `json:"state"`
	Detail    string    `json:"detail,omitempty"`
	RunID     string    `json:"runId,omitempty"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Activity 把工作层 Run 的实时状态映射成界面上的头像环。
//
// 数据来源全部是既有事实：
//   - 正在进行 / 失败的 Run → agent.Manager
//   - 有待审批 → Run 的 waits
//
// 没有新的写入路径，所以不存在「状态不一致」。
type Activity struct {
	agents *agent.Manager

	mu sync.Mutex
	// speaking 谁正在群里发言。编排器在 start/end 事件里登记，
	// 让「正在打字」也亮成 working——这一步不是 Run，看不到 Run 表。
	speaking map[string]speakingEntry
}

type speakingEntry struct {
	convID string
	until  time.Time
}

func NewActivity(m *agent.Manager) *Activity {
	return &Activity{agents: m, speaking: make(map[string]speakingEntry)}
}

// SetSpeaking 标记某会话里某角色正在发言。
//
// 每收到一个 delta 就续期一次；超过 8 秒没有新 delta 自动失效——
// 上游断流时不能一直亮着「正在输入」。
func (a *Activity) SetSpeaking(convID, agentKey string) {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.speaking[convID+"|"+agentKey] = speakingEntry{
		convID: convID,
		until:  time.Now().Add(8 * time.Second),
	}
	a.mu.Unlock()
}

func (a *Activity) isSpeaking(convID, agentKey string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	e, ok := a.speaking[convID+"|"+agentKey]
	if !ok {
		return false
	}
	if time.Now().After(e.until) {
		delete(a.speaking, convID+"|"+agentKey)
		return false
	}
	return true
}

// ForConversation 返回某会话里每个角色的状态。
//
// agents 传会话的角色列表（由调用方从编排器的解析结果拿）。
func (a *Activity) ForConversation(convID string, agents []Agent) []AgentActivity {
	out := make([]AgentActivity, 0, len(agents))
	if a == nil || a.agents == nil {
		for _, ag := range agents {
			out = append(out, AgentActivity{Key: ag.Key, Name: ag.Name, State: ActIdle})
		}
		return out
	}

	// 一次性把工作区里所有 Run 摸出来，按操作者分组——
	// 每角色查一次会是 N 次全表扫描。
	runs := a.agents.RunsInConversation(convID)

	for _, ag := range agents {
		st := AgentActivity{Key: ag.Key, Name: ag.Name, State: ActIdle}
		now := time.Now()

		for _, r := range runs {
			if r.OperatorKey != ag.Key && r.OperatorKey != ag.Name {
				continue
			}
			switch {
			case r.Status == agent.StatusWaiting:
				// 待审批优先级最高：如果用户不点，整个任务就卡在这
				st.State = ActBlocked
				st.Detail = "等你审批"
				st.RunID = r.ID
				st.UpdatedAt = r.StartedAt
			case r.Status == agent.StatusRunning:
				if st.State != ActBlocked {
					st.State = ActWorking
					st.Detail = "正在处理：" + trimRunes(r.Task, 24)
					st.RunID = r.ID
					st.UpdatedAt = r.StartedAt
				}
			case r.Status == agent.StatusFailed && now.Sub(r.EndedAt) < 10*time.Minute:
				if st.State == ActIdle {
					st.State = ActBlocked
					st.Detail = "上次失败：" + trimRunes(r.Error, 30)
					st.RunID = r.ID
					st.UpdatedAt = r.EndedAt
				}
			case r.Status == agent.StatusDone && now.Sub(r.EndedAt) < 2*time.Minute:
				if st.State == ActIdle {
					st.State = ActDone
					st.Detail = "刚完成一个任务"
					st.RunID = r.ID
					st.UpdatedAt = r.EndedAt
				}
			}
		}

		// Run 表里没有，但正在群里发言 → 也是「在干活」
		if st.State == ActIdle && a.isSpeaking(convID, ag.Key) {
			st.State = ActWorking
			st.Detail = "正在发言"
			st.UpdatedAt = now
		}
		out = append(out, st)
	}
	return out
}

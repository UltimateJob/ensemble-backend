package service

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/agent"
	"github.com/UltimateJob/ensemble-backend/internal/domain"
	"github.com/UltimateJob/ensemble-backend/internal/store"
)

func newBridgeTestChat() (*Chat, *store.Memory) {
	st := store.NewMemory()
	// AppendRunMessage 只用 store；网关为 nil 足够。
	return NewChat(st, nil), st
}

func TestAppendRunMessageDone(t *testing.T) {
	c, st := newBridgeTestChat()
	const (
		convID = "conv_1"
		runID  = "run_1"
	)
	artifacts := []domain.ArtifactRef{
		{
			ID:        "art_1",
			Kind:      "file",
			Name:      "calc.go",
			Path:      "src/calc.go",
			Size:      128,
			AgentKey:  "backend",
			AgentName: "后端",
			RunID:     runID,
			CreatedAt: time.Now().Truncate(time.Second),
		},
		{
			ID:        "art_2",
			Kind:      "doc",
			Name:      "说明.docx",
			Path:      "docs/说明.docx",
			Size:      2048,
			AgentKey:  "backend",
			AgentName: "后端",
			RunID:     runID,
			CreatedAt: time.Now().Truncate(time.Second),
		},
	}
	r := &agent.Run{
		ID:             runID,
		ConversationID: convID,
		OperatorName:   "后端",
		Summary:        "改了 2 个文件",
		Status:         agent.StatusDone,
		Artifacts:      artifacts,
		Step:           3,
		TokensIn:       1200,
		TokensOut:      300,
		Model:          "m-a",
	}

	id := c.AppendRunMessage(r)
	if id == "" {
		t.Fatal("AppendRunMessage should return the persisted message id")
	}

	msgs := st.ListMessages(convID, 0)
	if len(msgs) != 1 {
		t.Fatalf("expected exactly one message, got %d", len(msgs))
	}
	m := msgs[0]
	if m.ID != id {
		t.Fatalf("returned id %q does not match stored message %q", id, m.ID)
	}
	if m.Role != domain.RoleAssistant {
		t.Fatalf("role = %q, want assistant", m.Role)
	}
	if m.Agent != "后端" {
		t.Fatalf("agent = %q, want 后端", m.Agent)
	}
	if m.Kind != "run" {
		t.Fatalf("kind = %q, want run", m.Kind)
	}
	if m.Ref != runID {
		t.Fatalf("ref = %q, want %q", m.Ref, runID)
	}
	if m.Content != "改了 2 个文件" {
		t.Fatalf("content = %q, want summary", m.Content)
	}
	if !reflect.DeepEqual(m.Attachments, artifacts) {
		t.Fatalf("attachments were not preserved: got %+v, want %+v", m.Attachments, artifacts)
	}
	if m.TokensIn != 1200 || m.TokensOut != 300 || m.Model != "m-a" {
		t.Fatalf("usage fields not copied: %+v", m)
	}
}

func TestAppendRunMessageFailedKeepsReadableContent(t *testing.T) {
	cases := []struct {
		name   string
		status agent.Status
		errMsg string
	}{
		{name: "failed", status: agent.StatusFailed, errMsg: "模型返回空响应"},
		{name: "aborted", status: agent.StatusAborted, errMsg: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, st := newBridgeTestChat()
			r := &agent.Run{
				ID:             "run_" + tc.name,
				ConversationID: "conv_failed",
				OperatorName:   "后端",
				Status:         tc.status,
				Error:          tc.errMsg,
				// Summary 故意留空：仍要有可读内容。
			}
			id := c.AppendRunMessage(r)
			if id == "" {
				t.Fatal("AppendRunMessage should return an id for a failed run")
			}
			msgs := st.ListMessages("conv_failed", 0)
			if len(msgs) != 1 {
				t.Fatalf("expected one message, got %d", len(msgs))
			}
			m := msgs[0]
			if m.Kind != "run.failed" {
				t.Fatalf("kind = %q, want run.failed", m.Kind)
			}
			if strings.TrimSpace(m.Content) == "" {
				t.Fatal("failed run message must have readable content")
			}
			if !strings.Contains(m.Content, "任务没有完成") {
				t.Fatalf("content %q should explain the failure", m.Content)
			}
			if tc.errMsg != "" && !strings.Contains(m.Content, tc.errMsg) {
				t.Fatalf("content %q should include error %q", m.Content, tc.errMsg)
			}
		})
	}
}

func TestAppendRunMessageWithoutConversation(t *testing.T) {
	c, st := newBridgeTestChat()
	if id := c.AppendRunMessage(&agent.Run{ID: "run_orphan", Status: agent.StatusDone}); id != "" {
		t.Fatalf("expected empty id, got %q", id)
	}
	if id := c.AppendRunMessage(nil); id != "" {
		t.Fatalf("expected empty id for nil run, got %q", id)
	}
	if got := st.CountMessages(""); got != 0 {
		t.Fatalf("expected no messages appended, got %d", got)
	}
}

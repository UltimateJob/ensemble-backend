package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
)

// fakeConnectors 模拟一组连接器动作。
type fakeConnectors struct {
	actions []ConnectorAction
	calls   []string
	out     string
	err     error
}

func (f *fakeConnectors) Actions(string) []ConnectorAction { return f.actions }
func (f *fakeConnectors) Run(_ context.Context, _, connID, action string, _ map[string]any) (string, error) {
	f.calls = append(f.calls, connID+"/"+action)
	return f.out, f.err
}

// 没配连接器时，工具列表里不该出现任何外部工具。
// 否则模型会去调不存在的 github_search_code，白烧一轮。
func TestToolsetWithoutConnectors(t *testing.T) {
	m := NewManager()
	tools, idx := m.toolsetFor("u1", nil)

	if len(idx) != 0 {
		t.Fatalf("没配连接器却出现了动作：%v", idx)
	}
	for _, tl := range tools {
		if strings.Contains(tl.Name, "__") {
			t.Fatalf("出现连接器工具：%s", tl.Name)
		}
	}
	if len(tools) != 9 {
		t.Fatalf("内置工具应为 9 个，实际 %d", len(tools))
	}
}

// 配了连接器后，动作要出现在工具列表里，并且能被索引到。
func TestToolsetWithConnectors(t *testing.T) {
	m := NewManager()
	m.SetConnectors(&fakeConnectors{actions: []ConnectorAction{
		{Name: "github_search_code__c1", Description: "[我的仓库] 搜代码", Risk: "read", ConnID: "c1", Base: "github_search_code"},
		{Name: "notion_get_page__c2", Description: "[知识库] 读页面", Risk: "read", ConnID: "c2", Base: "notion_get_page"},
	}})

	tools, idx := m.toolsetFor("u1", nil)
	if len(idx) != 2 {
		t.Fatalf("应有 2 个动作，实际 %d", len(idx))
	}
	if len(tools) != 11 {
		t.Fatalf("工具总数应为 11（9 内置 + 2 连接器），实际 %d", len(tools))
	}

	// 工具名必须和索引键一致——模型回调时用的是工具名
	found := false
	for _, tl := range tools {
		if tl.Name == "github_search_code__c1" {
			found = true
			if !strings.Contains(tl.Description, "我的仓库") {
				t.Fatal("描述里应带连接器名，方便模型分辨同类多个实例")
			}
		}
	}
	if !found {
		t.Fatal("连接器工具没有出现在工具列表里")
	}
}

// 连接器调用要真的走到执行器，参数要原样传下去。
func TestRunConnectorDispatches(t *testing.T) {
	fc := &fakeConnectors{out: "仓库里有 3 个 issue"}
	m := NewManager()
	m.SetConnectors(fc)

	args, _ := json.Marshal(map[string]any{"q": "panic"})
	got, err := m.runConnector(context.Background(), "u1",
		ConnectorAction{ConnID: "c1", Base: "github_search_code"}, string(args))
	if err != nil {
		t.Fatal(err)
	}
	if got != "仓库里有 3 个 issue" {
		t.Fatalf("返回值不对：%q", got)
	}
	if len(fc.calls) != 1 || fc.calls[0] != "c1/github_search_code" {
		t.Fatalf("没有正确分发：%v", fc.calls)
	}
}

// 参数不是合法 JSON 时要明确报错，不能带着半个参数去调外部 API。
func TestRunConnectorRejectsBadArgs(t *testing.T) {
	m := NewManager()
	m.SetConnectors(&fakeConnectors{})
	_, err := m.runConnector(context.Background(), "u1",
		ConnectorAction{ConnID: "c1", Base: "x"}, "{not json")
	if err == nil {
		t.Fatal("坏参数应该报错")
	}
}

// 生图工具必须同时满足两个条件：配了渠道 + 角色被授予 image 能力。
//
// 这条是本次改动的核心约束。以前只看「配没配渠道」，结果是一旦运维
// 加了生图渠道，写文案的角色也能悄悄调用 image_generate 花掉额度。
func TestImageToolRequiresCapability(t *testing.T) {
	m := NewManager()
	m.SetImageGen(&fakeImageRunner{})

	// 配了渠道，但角色没有 image 能力：不该出现生图工具
	tools, _ := m.toolsetFor("u1", nil)
	if hasTool(tools, ToolImage) {
		t.Fatal("没授予 image 能力的角色拿到了 image_generate")
	}

	// 授予能力后才有
	tools, _ = m.toolsetFor("u1", []string{"image"})
	if !hasTool(tools, ToolImage) {
		t.Fatal("授予 image 能力后仍然没有 image_generate")
	}

	// 没配渠道时，即使有权限也不给——调了只会报错
	m2 := NewManager()
	tools, _ = m2.toolsetFor("u1", []string{"image"})
	if hasTool(tools, ToolImage) {
		t.Fatal("没配生图渠道却给了 image_generate")
	}
}

func hasTool(tools []domain.Tool, name string) bool {
	for _, t := range tools {
		if t.Name == name {
			return true
		}
	}
	return false
}

type fakeImageRunner struct{}

func (f *fakeImageRunner) Generate(ctx context.Context, userID, root, prompt, size string, n int) ([]ImageFile, error) {
	return nil, nil
}

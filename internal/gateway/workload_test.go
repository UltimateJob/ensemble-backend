package gateway

import (
	"testing"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
)

// workload 是偏好不是约束：标了合适任务的排前面，
// 没标的排中间，标了别类的排最后。
func TestSortByWorkloadOrdering(t *testing.T) {
	list := []domain.ModelInfo{
		{Key: "other", Workloads: []domain.Workload{domain.WorkloadChat}},
		{Key: "unlabeled"},
		{Key: "match", Workloads: []domain.Workload{domain.WorkloadCode}},
	}
	sortByWorkload(list, domain.WorkloadCode)

	got := []string{list[0].Key, list[1].Key, list[2].Key}
	want := []string{"match", "unlabeled", "other"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("排序不对：%v，期望 %v", got, want)
		}
	}
}

// 同为「匹配」时保持原有相对顺序——rank / weight 的既定策略不能被
// workload 排序打乱。
func TestSortByWorkloadIsStable(t *testing.T) {
	list := []domain.ModelInfo{
		{Key: "a", Workloads: []domain.Workload{domain.WorkloadCode}},
		{Key: "b", Workloads: []domain.Workload{domain.WorkloadCode}},
		{Key: "c", Workloads: []domain.Workload{domain.WorkloadCode}},
	}
	sortByWorkload(list, domain.WorkloadCode)
	for i, want := range []string{"a", "b", "c"} {
		if list[i].Key != want {
			t.Fatalf("稳定排序被破坏：%v", []string{list[0].Key, list[1].Key, list[2].Key})
		}
	}
}

// 全部标成别类任务时也要能选出来一个——不能因为标签不匹配就没有候选。
func TestCandidatesForNeverStarvesPool(t *testing.T) {
	tbl := NewModelTable([]domain.ModelInfo{
		{Key: "only-chat", Enabled: true, Workloads: []domain.Workload{domain.WorkloadChat}},
	})
	cands, ok := tbl.CandidatesFor("", false, domain.WorkloadCode)
	if !ok || len(cands) == 0 {
		t.Fatal("标签不匹配时不该让候选池变空")
	}
}

// 空 workload 不参与排序，行为与 Candidates 一致。
func TestCandidatesForEmptyWorkloadIsNoop(t *testing.T) {
	tbl := NewModelTable([]domain.ModelInfo{
		{Key: "b", Enabled: true, Rank: 1},
		{Key: "a", Enabled: true, Rank: 2},
	})
	cands, ok := tbl.CandidatesFor("", false, "")
	if !ok || len(cands) != 2 {
		t.Fatalf("应有 2 个候选，实际 %d", len(cands))
	}
	if cands[0].Key != "b" {
		t.Fatalf("rank 小的应排在前，实际 %s", cands[0].Key)
	}
}

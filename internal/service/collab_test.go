package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
	"github.com/UltimateJob/ensemble-backend/internal/store"
)

/* ---------------- 目标看板 ---------------- */

func TestGoalCRUDAndOwnership(t *testing.T) {
	st := store.NewMemory()
	g := NewGoals(st)

	v, err := g.Create("u1", domain.Goal{Title: "  园区改造标书交付  "})
	if err != nil {
		t.Fatal(err)
	}
	if v.Title != "园区改造标书交付" {
		t.Fatalf("标题没 trim：%q", v.Title)
	}
	if v.Status != domain.GoalTodo {
		t.Fatalf("默认状态应该是 todo，实际 %q", v.Status)
	}
	if v.ProgressSource != "status" {
		t.Fatalf("默认进度来源应该是 status，实际 %q", v.ProgressSource)
	}

	// 别人看不到、改不了
	if got := g.List("u2"); len(got) != 0 {
		t.Fatal("别的用户看到了不属于自己的目标")
	}
	if _, err := g.Update("u2", v.ID, GoalPatch{}); err != ErrGoalNotFound {
		t.Fatalf("越权更新应该报 not found，实际 %v", err)
	}
	if err := g.Delete("u2", v.ID); err != ErrGoalNotFound {
		t.Fatalf("越权删除应该报 not found，实际 %v", err)
	}
}

func TestGoalProgressDerivation(t *testing.T) {
	st := store.NewMemory()
	g := NewGoals(st)

	// 手动值优先
	manual := 42
	v, _ := g.Create("u1", domain.Goal{Title: "手动", ManualProgress: manual})
	if v.Progress != 42 || v.ProgressSource != "manual" {
		t.Fatalf("手动进度没生效：%d %s", v.Progress, v.ProgressSource)
	}

	// 完成即 100
	v2, _ := g.Create("u1", domain.Goal{Title: "完成", Status: domain.GoalDone})
	if v2.Progress != 100 || v2.ProgressSource != "status" {
		t.Fatalf("完成的目标进度应该是 100：%d", v2.Progress)
	}

	// 进行中且有 Run
	doing := domain.GoalDoing
	v3, _ := g.Create("u1", domain.Goal{Title: "进行", Status: doing, RunIDs: []string{"run_1"}})
	if v3.Progress != 60 || v3.ProgressSource != "runs" {
		t.Fatalf("进行中带 Run 的进度应为 60：%d %s", v3.Progress, v3.ProgressSource)
	}
}

func TestGoalValidation(t *testing.T) {
	g := NewGoals(store.NewMemory())
	if _, err := g.Create("u1", domain.Goal{Title: "  "}); err == nil {
		t.Fatal("空标题应该被拒")
	}
	if _, err := g.Create("u1", domain.Goal{Title: strings.Repeat("长", 101)}); err == nil {
		t.Fatal("超长标题应该被拒")
	}
	v, _ := g.Create("u1", domain.Goal{Title: "正常"})
	bad := 101
	if _, err := g.Update("u1", v.ID, GoalPatch{ManualProgress: &bad}); err == nil {
		t.Fatal("越界进度应该被拒")
	}
	badStatus := "unknown"
	if _, err := g.Update("u1", v.ID, GoalPatch{Status: &badStatus}); err == nil {
		t.Fatal("非法状态应该被拒")
	}
}

func TestGoalDoneSetsCompletedAt(t *testing.T) {
	g := NewGoals(store.NewMemory())
	v, _ := g.Create("u1", domain.Goal{Title: "某个目标"})
	if !v.CompletedAt.IsZero() {
		t.Fatal("新建的目标不该有完成时间")
	}
	done := domain.GoalDone
	v2, _ := g.Update("u1", v.ID, GoalPatch{Status: &done})
	if v2.CompletedAt.IsZero() {
		t.Fatal("完成时应该记录完成时间")
	}
	todo := domain.GoalTodo
	v3, _ := g.Update("u1", v.ID, GoalPatch{Status: &todo})
	if !v3.CompletedAt.IsZero() {
		t.Fatal("改回未完成时应该清掉完成时间")
	}
}

/* ---------------- 连接器 ---------------- */

func TestConnectorSecretsNeverReturned(t *testing.T) {
	st := store.NewMemory()
	c := NewConnectors(st)

	created, err := c.Create("u1", domain.Connector{
		Kind: "github", Name: "我的 GitHub",
		Secrets: map[string]string{"token": "ghp_secret_value"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Secrets != nil {
		t.Fatal("创建响应里带出了明文凭据")
	}
	list := c.List("u1")
	if len(list) != 1 {
		t.Fatalf("应该有一个连接器，实际 %d", len(list))
	}
	if list[0].Secrets != nil {
		t.Fatal("列表响应里带出了明文凭据")
	}
	// 但 SecretKeys 要能告诉界面「填过了」
	if len(list[0].SecretKeys) != 1 || list[0].SecretKeys[0] != "token" {
		t.Fatalf("SecretKeys 不对：%v", list[0].SecretKeys)
	}
	// 序列化后也不能有明文
	b, _ := json.Marshal(list[0])
	if strings.Contains(string(b), "ghp_secret_value") {
		t.Fatal("JSON 序列化后泄露了明文凭据")
	}
}

func TestConnectorProbeGitHub(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"login":"octocat"}`)
	}))
	defer srv.Close()

	st := store.NewMemory()
	c := NewConnectors(st)
	c.SetProbe(HTTPProbe{Client: srv.Client()})

	created, _ := c.Create("u1", domain.Connector{
		Kind: "github",
		Config: map[string]string{
			"baseUrl": srv.URL,
		},
		Secrets: map[string]string{"token": "ghp_test"},
	})
	got, err := c.Probe(context.Background(), "u1", created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.ConnStatusOK {
		t.Fatalf("探测应该成功，实际 %s：%s", got.Status, got.LastError)
	}
	if gotAuth != "Bearer ghp_test" {
		t.Fatalf("没带上凭据，实际头：%q", gotAuth)
	}
}

func TestConnectorProbeReportsUpstreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"message":"Bad credentials"}`)
	}))
	defer srv.Close()

	st := store.NewMemory()
	c := NewConnectors(st)
	c.SetProbe(HTTPProbe{Client: srv.Client()})

	created, _ := c.Create("u1", domain.Connector{
		Kind:    "github",
		Config:  map[string]string{"baseUrl": srv.URL},
		Secrets: map[string]string{"token": "bad"},
	})
	got, err := c.Probe(context.Background(), "u1", created.ID)
	if err != nil {
		t.Fatal(err)
	}
	// 探测本身成功执行，但结果要如实反映上游拒绝
	if got.Status != domain.ConnStatusError {
		t.Fatalf("上游 401 时状态应为 error，实际 %s", got.Status)
	}
	if !strings.Contains(got.LastError, "拒绝了这组凭据") {
		t.Fatalf("错误文案应该说明是被拒：%q", got.LastError)
	}
}

func TestConnectorProbeMissingSecret(t *testing.T) {
	st := store.NewMemory()
	c := NewConnectors(st)
	created, _ := c.Create("u1", domain.Connector{Kind: "github"})
	got, err := c.Probe(context.Background(), "u1", created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.ConnStatusError {
		t.Fatalf("没填凭据时状态应为 error，实际 %s", got.Status)
	}
	if !strings.Contains(got.LastError, "还没填") {
		t.Fatalf("错误文案应该提示填凭据：%q", got.LastError)
	}
}

func TestConnectorSecretMerge(t *testing.T) {
	st := store.NewMemory()
	c := NewConnectors(st)
	created, _ := c.Create("u1", domain.Connector{
		Kind: "feishu",
		Secrets: map[string]string{
			"appId": "id1", "appSecret": "sec1",
		},
	})
	// 只改一个字段，另一个应该保留
	patch := map[string]string{"appId": "id2"}
	c.Update("u1", created.ID, ConnPatch{Secrets: &patch})

	cur, _ := st.GetConnector(created.ID)
	if cur.Secrets["appId"] != "id2" {
		t.Fatalf("appId 没更新：%q", cur.Secrets["appId"])
	}
	if cur.Secrets["appSecret"] != "sec1" {
		t.Fatalf("appSecret 被误删：%q", cur.Secrets["appSecret"])
	}
	// 空字符串表示清除
	clear := map[string]string{"appSecret": ""}
	c.Update("u1", created.ID, ConnPatch{Secrets: &clear})
	cur, _ = st.GetConnector(created.ID)
	if _, exists := cur.Secrets["appSecret"]; exists {
		t.Fatal("空字符串没有清除字段")
	}
}

func TestConnectorRejectsUnknownKind(t *testing.T) {
	c := NewConnectors(store.NewMemory())
	if _, err := c.Create("u1", domain.Connector{Kind: "ftp"}); err != ErrConnKind {
		t.Fatalf("未知类型应该被拒，实际 %v", err)
	}
}

/* ---------------- 凭据加密 ---------------- */

func TestConnectorSealedOnDisk(t *testing.T) {
	dir := t.TempDir()
	if err := store.SetSecretKey("test-jwt-secret-for-sealing"); err != nil {
		t.Fatal(err)
	}
	fs, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()

	c := NewConnectors(fs)
	created, _ := c.Create("u1", domain.Connector{
		Kind: "github", Secrets: map[string]string{"token": "ghp_plaintext_marker"},
	})
	_ = fs.UpdateConnector(mustConnector(t, fs, created.ID))
	fs.Flush()

	raw, err := os.ReadFile(dir + "/connectors.json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "ghp_plaintext_marker") {
		t.Fatal("凭据明文写进了 connectors.json")
	}
	// 文件权限应收紧
	info, _ := os.Stat(dir + "/connectors.json")
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("connectors.json 权限太宽：%v", info.Mode().Perm())
	}
}

func mustConnector(t *testing.T, st store.Store, id string) *domain.Connector {
	t.Helper()
	c, ok := st.GetConnector(id)
	if !ok {
		t.Fatal("连接器不存在")
	}
	return c
}

// 回归测试：密钥必须在打开存储之前注入。
//
// 曾经把 SetSecretKey 放在 NewFileStore 之后，导致重启后所有连接器
// 都显示「解密失败」——加载时用的是兜底密钥。这个测试按真实启动
// 顺序走一遍，确保加载阶段就能解开。
func TestConnectorSurvivesRestartWithKey(t *testing.T) {
	dir := t.TempDir()
	const secret = "restart-test-jwt-secret"

	// 第一次启动：注入密钥 → 打开存储 → 写连接器 → 落盘 → 关闭
	if err := store.SetSecretKey(secret); err != nil {
		t.Fatal(err)
	}
	fs1, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	c1 := NewConnectors(fs1)
	created, err := c1.Create("u1", domain.Connector{
		Kind: "github", Secrets: map[string]string{"token": "ghp_persist_me"},
	})
	if err != nil {
		t.Fatal(err)
	}
	fs1.Flush()
	fs1.Close()

	// 第二次启动：同样的顺序（先注入密钥，再打开存储）
	if err := store.SetSecretKey(secret); err != nil {
		t.Fatal(err)
	}
	fs2, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer fs2.Close()

	got, ok := fs2.GetConnector(created.ID)
	if !ok {
		t.Fatal("重启后连接器丢了")
	}
	if got.Secrets["token"] != "ghp_persist_me" {
		t.Fatalf("重启后凭据没解开：%q（status=%s err=%s）",
			got.Secrets["token"], got.Status, got.LastError)
	}
	if got.Status == domain.ConnStatusError {
		t.Fatalf("重启后连接器状态不该是 error：%s", got.LastError)
	}
}

// 换密钥后应明确失败（而不是静默当成没填）。
func TestConnectorWrongKeyMarkedError(t *testing.T) {
	dir := t.TempDir()
	if err := store.SetSecretKey("first-key"); err != nil {
		t.Fatal(err)
	}
	fs1, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	c1 := NewConnectors(fs1)
	created, _ := c1.Create("u1", domain.Connector{
		Kind: "github", Secrets: map[string]string{"token": "secret"},
	})
	fs1.Flush()
	fs1.Close()

	// 换了 JWT_SECRET 再启动
	if err := store.SetSecretKey("second-key"); err != nil {
		t.Fatal(err)
	}
	fs2, err := store.NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer fs2.Close()

	got, ok := fs2.GetConnector(created.ID)
	if !ok {
		t.Fatal("连接器记录不该消失")
	}
	if got.Status != domain.ConnStatusError {
		t.Fatalf("换密钥后应标为 error，实际 %s", got.Status)
	}
	if !strings.Contains(got.LastError, "解密失败") {
		t.Fatalf("错误信息应说明是解密失败：%q", got.LastError)
	}
}

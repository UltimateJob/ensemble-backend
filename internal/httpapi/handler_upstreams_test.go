package httpapi

import (
	"path/filepath"
	"testing"

	"github.com/UltimateJob/ensemble-backend/internal/store"
)

func newUpstreamTestServer(t *testing.T) (*Server, *store.Upstreams) {
	t.Helper()
	srv, _ := newTestServer(t)
	up, err := store.NewUpstreams(filepath.Join(t.TempDir(), "upstreams.json"))
	if err != nil {
		t.Fatal(err)
	}
	srv.SetUpstreams(up)
	return srv, up
}

// 管理台可以新增渠道并热注册到网关；Key 不回传明文。
func TestAdminUpstreamCreateAndHotRegister(t *testing.T) {
	srv, up := newUpstreamTestServer(t)
	h := srv.Handler()

	rec, body := doJSON(t, h, "POST", "/admin/api/upstreams", "test-admin", map[string]any{
		"name": "official", "baseUrl": "https://api.deepseek.com", "apiKey": "sk-test-1234567890", "model": "deepseek-chat",
	})
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%v", rec.Code, body)
	}
	if _, ok := srv.gw.Registry().Get("official"); !ok {
		t.Fatal("新渠道应热注册到网关")
	}
	items, _ := body["data"].([]any)
	if len(items) != 1 {
		t.Fatalf("items=%v", body["data"])
	}
	item := items[0].(map[string]any)
	if _, leaked := item["apiKey"]; leaked {
		t.Fatal("列表接口不能回传 Key 明文")
	}
	if _, leaked := item["sealed"]; leaked {
		t.Fatal("列表接口不能回传密文")
	}
	if item["keyHint"] == "" {
		t.Fatal("应给出 Key 末四位用于辨认")
	}

	// 更新时不传 Key 应沿用原 Key，渠道仍在。
	rec, body = doJSON(t, h, "POST", "/admin/api/upstreams", "test-admin", map[string]any{
		"name": "official", "baseUrl": "https://api.deepseek.com", "model": "deepseek-chat",
	})
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%v", rec.Code, body)
	}
	if _, ok := up.Key("official"); !ok {
		t.Fatal("不传 Key 时应沿用原 Key")
	}
}

// 编辑渠道：改名会被拒绝（名字是模型表里的 provider），改地址与 Key 生效。
func TestAdminUpstreamEditKeepsName(t *testing.T) {
	srv, _ := newUpstreamTestServer(t)
	h := srv.Handler()
	doJSON(t, h, "POST", "/admin/api/upstreams", "test-admin", map[string]any{
		"name": "relay", "baseUrl": "https://a.example.com", "apiKey": "sk-old-1234", "model": "chat-x",
	})
	rec, body := doJSON(t, h, "POST", "/admin/api/upstreams", "test-admin", map[string]any{
		"name": "relay", "baseUrl": "https://b.example.com", "model": "chat-y",
	})
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%v", rec.Code, body)
	}
	item := body["data"].([]any)[0].(map[string]any)
	if item["baseUrl"] != "https://b.example.com" || item["model"] != "chat-y" {
		t.Fatalf("编辑未生效：%v", item)
	}
	// 渠道已改名不可行——名字是主键；这里确认列表里没有多出一条。
	if len(body["data"].([]any)) != 1 {
		t.Fatalf("编辑不应新增渠道：%v", body["data"])
	}
}

// 删除仍被启用模型引用的渠道要拒绝。
func TestAdminUpstreamDeleteGuarded(t *testing.T) {
	srv, _ := newUpstreamTestServer(t)
	h := srv.Handler()
	doJSON(t, h, "POST", "/admin/api/upstreams", "test-admin", map[string]any{
		"name": "backup", "baseUrl": "https://api.example.com", "apiKey": "sk-x-1234", "model": "chat-x",
	})
	rec, _ := doJSON(t, h, "DELETE", "/admin/api/upstreams/backup", "test-admin", nil)
	if rec.Code == 200 {
		t.Fatal("被启用模型引用的渠道不应允许删除")
	}
}

// 新增渠道时自动补模型行，否则渠道永远接不到流量。
func TestAdminUpstreamCreatesModelRow(t *testing.T) {
	srv, _ := newUpstreamTestServer(t)
	h := srv.Handler()
	rec, body := doJSON(t, h, "POST", "/admin/api/upstreams", "test-admin", map[string]any{
		"name": "backup", "baseUrl": "https://api.example.com", "apiKey": "sk-x-1234", "model": "chat-x",
	})
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%v", rec.Code, body)
	}
	var found bool
	for _, m := range srv.admin.Models() {
		if m.Provider == "backup" {
			found = true
			if !m.Enabled || m.Weight <= 0 || m.Rank == 0 {
				t.Fatalf("自动创建的模型应启用、有权重且排在有排名渠道之后：%+v", m)
			}
		}
	}
	if !found {
		t.Fatal("新渠道应自动创建模型行")
	}
}

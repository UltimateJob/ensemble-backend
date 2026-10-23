package httpapi

import (
	"path/filepath"
	"testing"

	"github.com/UltimateJob/ensemble-backend/internal/store"
)

// 管理台可以新增渠道并热注册到网关；Key 不回传明文。
func TestAdminUpstreamCreateAndHotRegister(t *testing.T) {
	srv, h := newTestServer(t)
	up, err := store.NewUpstreams(filepath.Join(t.TempDir(), "upstreams.json"))
	if err != nil {
		t.Fatal(err)
	}
	srv.SetUpstreams(up, 0)

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

// 删除仍被启用模型引用的渠道要拒绝。
func TestAdminUpstreamDeleteGuarded(t *testing.T) {
	srv, h := newTestServer(t)
	up, err := store.NewUpstreams(filepath.Join(t.TempDir(), "upstreams.json"))
	if err != nil {
		t.Fatal(err)
	}
	srv.SetUpstreams(up, 0)
	srv.SetProviderDeleteGuard(func(name string) string {
		if name == "mock" {
			return "还有启用的模型"
		}
		return ""
	})
	rec, _ := doJSON(t, h, "DELETE", "/admin/api/upstreams/mock", "test-admin", nil)
	if rec.Code == 200 {
		t.Fatal("被模型引用的渠道不应允许删除")
	}
}

// 新增渠道时自动补模型行，否则渠道永远接不到流量。
func TestAdminUpstreamCreatesModelRow(t *testing.T) {
	srv, h := newTestServer(t)
	up, err := store.NewUpstreams(filepath.Join(t.TempDir(), "upstreams.json"))
	if err != nil {
		t.Fatal(err)
	}
	srv.SetUpstreams(up, 0)
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
			if !m.Enabled || m.Rank == 0 {
				t.Fatalf("自动创建的模型应启用且有排名：%+v", m)
			}
		}
	}
	if !found {
		t.Fatal("新渠道应自动创建模型行")
	}
}

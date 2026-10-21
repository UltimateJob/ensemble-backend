package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSearchClient(t *testing.T) {
	var got map[string]interface{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/tools/search" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("auth = %s", r.Header.Get("Authorization"))
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"results":[{"title":"漩涡鸣人 - 火影忍者","url":"https://example.com/naruto","snippet":"木叶忍者","summary":"漩涡鸣人是九尾人柱力，梦想成为火影。","content":"详细内容","site_name":"Example Wiki","date":"2026-01-01T00:00:00Z","authority":3}]}`)
	}))
	defer ts.Close()

	c := NewSearchClient(ts.URL, "test-key", 5*time.Second)
	results, err := c.Search(context.Background(), "漩涡鸣人 火影忍者", 6)
	if err != nil {
		t.Fatal(err)
	}
	if got["query"] != "漩涡鸣人 火影忍者" || int(got["count"].(float64)) != 6 || got["need_content"] != true {
		t.Fatalf("请求体不对：%+v", got)
	}
	if len(results) != 1 || results[0].SiteName != "Example Wiki" {
		t.Fatalf("结果不对：%+v", results)
	}
	if sources := toSources(results); len(sources) != 1 || sources[0].URL != "https://example.com/naruto" {
		t.Fatalf("来源不对：%+v", sources)
	}
}

func TestBuildSearchQuery(t *testing.T) {
	q := buildSearchQuery("漩涡鸣人", "火影忍者", "anime", "疾风传时期")
	for _, want := range []string{"漩涡鸣人", "火影忍者", "疾风传时期", "动漫", "性格"} {
		if !strings.Contains(q, want) {
			t.Fatalf("query 缺少 %q：%s", want, q)
		}
	}
	if len([]rune(q)) > 128 {
		t.Fatalf("query 超过 128 字：%d", len([]rune(q)))
	}
}

func TestFormatSearchBlock(t *testing.T) {
	block := formatSearchBlock([]SearchResult{{
		Title: "漩涡鸣人", URL: "https://example.com", SiteName: "Wiki", Summary: "九尾人柱力",
	}})
	for _, want := range []string{"[联网检索结果]", "漩涡鸣人", "https://example.com", "九尾人柱力"} {
		if !strings.Contains(block, want) {
			t.Fatalf("block 缺少 %q：\n%s", want, block)
		}
	}
}

func TestToSourcesDedupeByPath(t *testing.T) {
	got := toSources([]SearchResult{
		{Title: "A", URL: "https://example.com/item/1?from=a", SiteName: "Site A"},
		{Title: "A2", URL: "https://example.com/item/1?from=b", SiteName: "Site A"},
		{Title: "B", URL: "https://example.com/item/2", SiteName: "Site B"},
	})
	if len(got) != 2 {
		t.Fatalf("同一路径应去重，得到 %d：%+v", len(got), got)
	}
}

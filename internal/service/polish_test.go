package service

import (
	"strings"
	"testing"
)

func TestNormalizePolishKind(t *testing.T) {
	cases := map[string]string{
		"动漫": "anime", "anime": "anime", "电视剧": "tv", "tv": "tv",
		"电影": "movie", "明星": "celebrity", "游戏": "game", "": "other",
	}
	for in, want := range cases {
		if got := normalizePolishKind(in); got != want {
			t.Fatalf("%q -> %q，期望 %q", in, got, want)
		}
	}
}

func TestParsePolishJSON(t *testing.T) {
	raw := "```json\n{\"found\":true,\"confidence\":\"high\",\"canonicalName\":\"漩涡鸣人\",\"source\":\"火影忍者\",\"kind\":\"anime\",\"profile\":{\"identity\":\"木叶忍者\"},\"prompt\":\"" + strings.Repeat("人设", 50) + "\"}\n```"
	out, err := parsePolishJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !out.Found || out.CanonicalName != "漩涡鸣人" || out.Source != "火影忍者" {
		t.Fatalf("解析结果不对：%+v", out)
	}
}

func TestBuildPromptFromProfile(t *testing.T) {
	got := buildPromptFromProfile("漩涡鸣人", "火影忍者", PolishProfile{
		Era: "疾风传", Identity: "木叶忍者", Personality: "不服输",
		Speech: "口头禅是「说到做到」", EmotionalCore: "害怕被孤立",
		Boundaries: "不会放弃同伴", SampleLines: []string{"说到做到，这就是我的忍道！"},
	})
	for _, want := range []string{"漩涡鸣人", "火影忍者", "疾风传", "说到做到", "害怕被孤立", "不会放弃同伴"} {
		if !strings.Contains(got, want) {
			t.Fatalf("prompt 缺少 %q：\n%s", want, got)
		}
	}
}

func TestPolishPromptIncludesSource(t *testing.T) {
	got := polishUserPrompt("漩涡鸣人", "火影忍者", "anime", "疾风传时期", "本地库无", "[联网检索结果]\n百度百科")
	for _, want := range []string{"漩涡鸣人", "火影忍者", "疾风传时期", "联网检索结果", "百度百科"} {
		if !strings.Contains(got, want) {
			t.Fatalf("检索输入缺少 %q：\n%s", want, got)
		}
	}
}

func TestExactCharacter(t *testing.T) {
	if _, ok := exactCharacter("雪之下雪乃"); !ok {
		t.Fatal("本地角色库应精确命中雪之下雪乃")
	}
	if _, ok := exactCharacter("漩涡鸣人"); ok {
		t.Fatal("角色库未收录漩涡鸣人，应由模型检索")
	}
}

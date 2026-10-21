package service

import (
	"strings"
	"testing"
)

func TestRandomFriends(t *testing.T) {
	// 每次生成都应该是不同的人
	seen := map[string]bool{}
	distinct := 0
	for i := 0; i < 6; i++ {
		friends := RandomFriends(3)
		if len(friends) != 3 {
			t.Fatalf("应生成 3 个朋友，得到 %d", len(friends))
		}
		// 同一组内名字不重复
		names := map[string]bool{}
		for _, f := range friends {
			if names[f.Name] {
				t.Fatalf("同一组里名字重复了：%s", f.Name)
			}
			names[f.Name] = true
			if len([]rune(f.Prompt)) < 40 {
				t.Fatalf("%s 的人设太短（%d 字）", f.Name, len([]rune(f.Prompt)))
			}
			seen[f.Name] = true
		}
		distinct += len(names)
	}
	if len(seen) < 4 {
		t.Fatalf("6 次生成总共只出现 %d 个不同的人，随机性不足", len(seen))
	}
}

func TestSearchCharacters(t *testing.T) {
	// 按名字
	if got := SearchCharacters("雪之下雪乃"); len(got) == 0 || got[0].Name != "雪之下雪乃" {
		t.Fatal("按名字搜不到")
	}
	// 按标签
	if got := SearchCharacters("毒舌"); len(got) < 3 {
		t.Fatalf("按标签「毒舌」应能搜到多个，得到 %d", len(got))
	}
	// 按作品
	if got := SearchCharacters("轻音"); len(got) == 0 {
		t.Fatal("按作品搜不到")
	}
	// 空查询给一批让用户看见有什么
	if got := SearchCharacters(""); len(got) < 10 {
		t.Fatalf("空查询应返回一批，得到 %d", len(got))
	}
	// 搜不到就是空
	if got := SearchCharacters("绝对不存在的角色xyz"); len(got) != 0 {
		t.Fatalf("搜不到应返回空，得到 %d", len(got))
	}
}

func TestCharacterPrompt(t *testing.T) {
	chars := SearchCharacters("雪之下雪乃")
	if len(chars) == 0 {
		t.Fatal("角色库缺少测试数据")
	}
	title, prompt := CharacterPrompt(chars[0])
	if title == "" || prompt == "" {
		t.Fatal("生成的人设不能为空")
	}
	if len([]rune(prompt)) < 60 {
		t.Fatalf("人设太短：%d 字", len([]rune(prompt)))
	}
	// 必须带上角色名和说话方式，否则模型不知道该像谁
	if !strings.Contains(prompt, chars[0].Name) {
		t.Fatal("人设里没写角色名")
	}
	if !strings.Contains(prompt, chars[0].Tone) {
		t.Fatal("人设里没写说话方式")
	}
}

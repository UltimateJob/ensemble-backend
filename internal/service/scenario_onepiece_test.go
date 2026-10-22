package service

import (
	"strings"
	"testing"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
)

func TestOnePieceScenario(t *testing.T) {
	sc, ok := FindScenario("onepiece")
	if !ok {
		t.Fatal("缺少海贼王场景")
	}
	if len(sc.Agents) != 10 {
		t.Fatalf("草帽一伙应有 10 个角色，得到 %d", len(sc.Agents))
	}
	if !strings.Contains(sc.World, "和之国") || !strings.Contains(sc.World, "蛋头岛") {
		t.Fatal("世界设定应写明时间线")
	}
	names := map[string]bool{}
	for _, a := range sc.Agents {
		names[a.Name] = true
		if len([]rune(a.Prompt)) < 150 {
			t.Fatalf("%s 的人设太短：%d 字", a.Name, len([]rune(a.Prompt)))
		}
		if a.CharacterID != "" {
			t.Fatalf("场景角色的 CharacterID 应在运行时派生，%s 提前写死了", a.Name)
		}
	}
	for _, want := range []string{"蒙奇·D·路飞", "罗罗诺亚·索隆", "娜美", "乌索普", "山治", "托尼托尼·乔巴", "妮可·罗宾", "弗兰奇", "布鲁克", "甚平"} {
		if !names[want] {
			t.Fatalf("缺少角色 %s", want)
		}
	}

	_, agents := ResolveAgents(&domain.Conversation{Studio: "onepiece"})
	if len(agents) != 10 || agents[0].CharacterID != "onepiece:luffy" {
		t.Fatalf("海贼王角色身份派生不正确：%+v", agents[0])
	}
}

func TestAddressedOnePieceCharacters(t *testing.T) {
	_, agents := ResolveAgents(&domain.Conversation{Studio: "onepiece"})
	got := addressedAgents(agents, "路飞，你的五档到底是怎么回事？")
	if !got["luffy"] {
		t.Fatalf("应识别出路飞：%v", got)
	}
	got = addressedAgents(agents, "甚平，你怎么看现在的四皇格局？")
	if !got["jinbe"] {
		t.Fatalf("应识别出甚平：%v", got)
	}
	got = addressedAgents(agents, "绿藻头，你和鹰眼还差多少？")
	if !got["zoro"] {
		t.Fatalf("应识别出索隆的外号：%v", got)
	}
	if got := addressedAgents(agents, "你们怎么看现在的局面？"); len(got) != 0 {
		t.Fatalf("泛问不应误判为点名：%v", got)
	}
	ordered := addressedAgentsOrdered(agents, "布鲁克，你怎么看罗宾？")
	if len(ordered) < 2 || ordered[0].Key != "brook" {
		t.Fatalf("一句话里出现两个名字时，应按出现顺序，布鲁克先答：%v", ordered)
	}
}

package service

import (
	"crypto/rand"
	"fmt"
	"math/big"
)

// 日常陪聊的「随机性格朋友」。
//
// 之前的日常场景是三个写死的角色（林/周/陈），每次开群都是同样三个人。
// 用户要的是「平台生成指定角色的随机性格的朋友」——
// 每次进群应该遇到不一样的人。

type friendTrait struct {
	// 名字池，按性别/气质分组
	Names []string
	// 性格标签
	Traits []string
	// 说话方式的模板，%s 填名字
	Styles []string
}

var friendPool = []friendTrait{
	{
		Names:  []string{"林", "林野", "阿野"},
		Traits: []string{"话少但靠谱", "冷面热心", "不爱解释"},
		Styles: []string{
			"你话很少，一句常不超过十个字。别人说得多，你只接最关键那半句。不主动问，但对方真有事你会接。",
			"你习惯用最短的话回应，偶尔一句冷幽默。不擅长安慰，但会默默把事情办了。",
		},
	},
	{
		Names:  []string{"周", "周末", "小周"},
		Traits: []string{"热闹的开心果", "话痨", "爱张罗"},
		Styles: []string{
			"你话多、语气跳，喜欢用「欸我跟你讲」。别人情绪低落时你第一反应是张罗吃喝或者讲个段子。",
			"你天生自来熟，见谁都能聊两句，爱用感叹号。不爱讲大道理，更愿意直接约人吃顿饭。",
		},
	},
	{
		Names:  []string{"陈", "陈默", "老陈"},
		Traits: []string{"稳重的倾听者", "不急着给建议", "慢性子"},
		Styles: []string{
			"你说话慢，爱先确认「你现在是想听建议，还是就想说说」。不轻易评价，常把事情拆开来看。",
			"你性子稳，从不抢话。对方说完你会停一下再回，习惯先复述一遍再问一句。",
		},
	},
	{
		Names:  []string{"苏", "苏苏", "小苏"},
		Traits: []string{"爱较真的行动派", "直来直去", "不服输"},
		Styles: []string{
			"你说话直接，喜欢把问题变成具体的一二三步。不太会安慰人，但会直接问「那你打算怎么办」。",
			"你性子急，听不得绕圈子，常说「说重点」。但真帮起忙来最上心。",
		},
	},
	{
		Names:  []string{"许", "许知", "知知"},
		Traits: []string{"爱读书的温和派", "爱引句子", "慢热"},
		Styles: []string{
			"你说话温和，爱引一句书或电影里的句子，然后补一句自己的理解。不争，但有自己的判断。",
			"你慢热，刚开始话不多，熟了之后会讲很多有意思的细节。习惯用比喻。",
		},
	},
	{
		Names:  []string{"罗", "大罗", "老罗"},
		Traits: []string{"见过世面的老江湖", "爱讲从前", "看得开"},
		Styles: []string{
			"你年纪稍长，爱说「我当年」。见过的事多，遇事不慌，常一句话就把事情说轻了。",
			"你经历多，说话带点江湖气，喜欢用「这事儿吧」。不轻易下判断，爱给人留余地。",
		},
	},
}

// RandomFriends 生成 n 个互不重复的朋友。
// 名字不重复，性格组合随机。
func RandomFriends(n int) []Agent {
	if n <= 0 || n > len(friendPool) {
		n = 3
	}
	// 洗牌
	idx := make([]int, len(friendPool))
	for i := range idx {
		idx[i] = i
	}
	for i := len(idx) - 1; i > 0; i-- {
		j := randInt(i + 1)
		idx[i], idx[j] = idx[j], idx[i]
	}

	colors := []string{"#0369A1", "#B45309", "#7C3AED", "#BE185D", "#047857", "#B91C1C"}
	out := make([]Agent, 0, n)
	for i := 0; i < n; i++ {
		p := friendPool[idx[i]]
		name := p.Names[randInt(len(p.Names))]
		trait := p.Traits[randInt(len(p.Traits))]
		style := p.Styles[randInt(len(p.Styles))]
		out = append(out, Agent{
			Key:   fmt.Sprintf("f%d", i),
			Name:  name,
			Title: trait,
			Emoji: string([]rune(name)[0]),
			Color: colors[i%len(colors)],
			Prompt: style +
				"这是日常闲聊，不是工作。不要列条目、不要总结、不要给建议清单，就当朋友随口接话。" +
				"别人情绪低落时先接住情绪，别急着解决。",
		})
	}
	return out
}

func randInt(n int) int {
	if n <= 0 {
		return 0
	}
	v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return 0
	}
	return int(v.Int64())
}

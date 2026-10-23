package service

import (
	"sort"
	"strings"
)

// MentionedAgents 找出用户显式 @ 到的角色，按出现位置排序。
//
// 与 addressedAgentsOrdered 的区别：
//   - 只认显式 `@` / `＠`，正文里偶然出现角色名不算；
//   - 返回的是「只让这些人回复」的依据，而不只是发言顺序。
func MentionedAgents(agents []Agent, text string) []*Agent {
	if strings.TrimSpace(text) == "" || len(agents) == 0 {
		return nil
	}
	type ref struct {
		agent *Agent
		pos   int
	}
	seen := map[string]bool{}
	var refs []ref
	runes := []rune(text)
	for i, r := range runes {
		if r != '@' && r != '＠' {
			continue
		}
		rest := strings.TrimLeft(string(runes[i+1:]), " \t")
		if rest == "" {
			continue
		}
		best := -1
		bestLen := 0
		for ai := range agents {
			if seen[agents[ai].Key] {
				continue
			}
			for _, n := range mentionNames(agents[ai]) {
				n = strings.TrimSpace(n)
				if n == "" || !hasRunePrefix(rest, n) {
					continue
				}
				if asciiWord(n) && !asciiBoundary(rest, n) {
					continue
				}
				if l := len([]rune(n)); l > bestLen {
					best = ai
					bestLen = l
				}
			}
		}
		if best < 0 {
			continue
		}
		a := &agents[best]
		seen[a.Key] = true
		refs = append(refs, ref{agent: a, pos: i})
	}
	sort.SliceStable(refs, func(i, j int) bool { return refs[i].pos < refs[j].pos })
	out := make([]*Agent, 0, len(refs))
	for _, r := range refs {
		out = append(out, r.agent)
	}
	return out
}

// AgentsByKeys 按 key 或名字取角色，保持客户端给的顺序。
func AgentsByKeys(agents []Agent, keys []string) []*Agent {
	if len(keys) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]*Agent, 0, len(keys))
	for _, k := range keys {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		for i := range agents {
			a := &agents[i]
			if a.Key != k && a.Name != k {
				continue
			}
			if seen[a.Key] {
				break
			}
			seen[a.Key] = true
			out = append(out, a)
			break
		}
	}
	return out
}

// resolveMentionTargets 优先用客户端结构化传来的 key；没有就解析正文里的 @。
func resolveMentionTargets(agents []Agent, keys []string, text string) []*Agent {
	if out := AgentsByKeys(agents, keys); len(out) > 0 {
		return out
	}
	return MentionedAgents(agents, text)
}

// hasAllMention 判断是不是在 @ 所有人。这种不进入定向模式。
func hasAllMention(text string) bool {
	for _, marker := range []string{
		"@所有人", "@大家", "@全员", "@每个人", "@all", "@All",
		"＠所有人", "＠大家", "＠全员", "＠每个人", "＠all", "＠All",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func mentionNames(a Agent) []string {
	names := []string{a.Name, a.Key}
	names = append(names, a.Aliases...)
	for _, part := range strings.Split(a.Name, "·") {
		if len([]rune(part)) >= 2 {
			names = append(names, part)
		}
	}
	// 去重 + 去空，长名字优先由调用方比较长度决定。
	seen := map[string]bool{}
	out := names[:0]
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	return out
}

func hasRunePrefix(s, prefix string) bool {
	sr := []rune(s)
	pr := []rune(prefix)
	if len(sr) < len(pr) {
		return false
	}
	return string(sr[:len(pr)]) == prefix
}

// asciiBoundary 避免 @qaBot 被当成 @qa。
func asciiBoundary(s, word string) bool {
	sr := []rune(s)
	wr := []rune(word)
	if len(sr) <= len(wr) {
		return true
	}
	return !isASCIIWordRune(sr[len(wr)])
}

func asciiWord(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !isASCIIWordRune(r) {
			return false
		}
	}
	return true
}

func isASCIIWordRune(r rune) bool {
	return r == '_' || r == '-' ||
		(r >= '0' && r <= '9') ||
		(r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z')
}

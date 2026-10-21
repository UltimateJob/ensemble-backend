package agent

import (
	"regexp"
	"sort"
	"strings"
)

// numberClaim 匹配「带单位的数字」——这类数字最容易被编。
//
// 不检查裸数字：日期、端口、版本号都是数字，全查会全是误报。
var numberClaim = regexp.MustCompile(`\d+(?:\.\d+)?\s*(?:条|个|次|步|秒|毫秒|ms|%|行|文件|用例|任务|处|次工具)`)

// UnsourcedNumbers 找出文档里「没有来源」的统计数字。
//
// 判据：这个数字必须原样出现在本次 Run 的工具输出里（read/run/grep 的结果）。
// 编出来的「12 条用例」「17 个文件」会被抓住；「2026」「:8099」不会。
func UnsourcedNumbers(docText string, toolOutputs []string) []string {
	claims := numberClaim.FindAllString(docText, -1)
	if len(claims) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var missing []string
	for _, c := range claims {
		norm := strings.Join(strings.Fields(c), "")
		if seen[norm] {
			continue
		}
		seen[norm] = true
		if !sourceHas(toolOutputs, norm) {
			missing = append(missing, c)
		}
	}
	sort.Strings(missing)
	return missing
}

// sourceHas 只比对数字本身，不要求单位一致。
//
// 严格比对「12条」会把输出里的 {"count":12} 判成没来源——
// 误报会打断正常生成，比漏判更糟。所以放宽到「这个数字在输出里出现过」。
func sourceHas(outputs []string, claim string) bool {
	num := digitsOnly(claim)
	if num == "" {
		return true
	}
	pat := regexp.MustCompile(`(^|[^0-9.])` + regexp.QuoteMeta(num) + `([^0-9.]|$)`)
	for _, o := range outputs {
		if pat.MatchString(o) {
			return true
		}
	}
	return false
}

func digitsOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= '0' && r <= '9') || r == '.' {
			b.WriteRune(r)
		} else if b.Len() > 0 {
			break
		}
	}
	return strings.TrimSuffix(b.String(), ".")
}

package config

import "fmt"

// 生产环境自检。
//
// 这几个默认值在本地很方便，但直接部署到公网就是灾难：
//   DEV_BYPASS=true      —— 任何人输入任意手机号都能登录
//   ADMIN_TOKEN=默认值   —— 任何人都能进运维台，改额度、看数据
//   没有 API Key         —— 对外服务却一直在说演示台词
//
// PROD=1 时这些必须是安全的，否则拒绝启动。
// 宁可起不来，也不要带着这些配置对外跑。

// DefaultAdminToken 是开发用的默认令牌。
const DefaultAdminToken = "admin-dev-token"

// ProdProblems 返回生产模式下所有不安全配置。
// 空切片表示可以启动。
func (c Config) ProdProblems() []string {
	if !c.Prod {
		return nil
	}
	var out []string

	if c.DevBypass {
		out = append(out, "DEV_BYPASS 必须是 false——开着的话任何人输任意验证码都能登录")
	}
	if c.AdminToken == DefaultAdminToken || c.AdminToken == "" {
		out = append(out, "ADMIN_TOKEN 还是默认值——任何人都能进运维台。换一个随机串")
	}
	if len(c.AdminToken) < 16 {
		out = append(out, fmt.Sprintf("ADMIN_TOKEN 太短（%d 字符）——建议至少 16 位", len(c.AdminToken)))
	}
	if !c.UpstreamConfigured() {
		out = append(out, "没有配置 DEEPSEEK_API_KEY——对外服务会一直返回演示台词")
	}
	if c.DataDir == "" {
		out = append(out, "DATA_DIR 为空")
	}
	return out
}

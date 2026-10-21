package workspace

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// 命令执行预算。所有值都可以被单次调用覆盖，但都有硬上限。
const (
	DefaultCommandTimeout = 120 * time.Second
	MaxCommandTimeout     = 10 * time.Minute
	MaxCommandOutput      = 256 << 10 // 256 KiB，超过截断并标记
)

// Command 一次命令执行请求。
type Command struct {
	// Argv 是首选形式：不经过 shell，没有注入面。
	Argv []string `json:"argv"`
	// Shell 为 true 时把 Argv 拼成一条命令交给 /bin/sh -c。
	// 仅用于兼容必须的管道/重定向，风险等级会被上调。
	Shell   bool              `json:"shell,omitempty"`
	Cwd     string            `json:"cwd,omitempty"` // 相对工作区路径，默认 root
	Timeout time.Duration     `json:"timeout,omitempty"`
	Env     map[string]string `json:"env,omitempty"` // 追加在最小环境之上
}

// CommandResult 执行结果。
type CommandResult struct {
	Command    string `json:"command"`
	ExitCode   int    `json:"exitCode"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	Truncated  bool   `json:"truncated"`
	TimedOut   bool   `json:"timedOut"`
	DurationMS int64  `json:"durationMs"`
	Risk       string `json:"risk"`
}

// 风险等级：决定审批策略与 UI 提示。
const (
	RiskRead      = "read"      // 只读命令
	RiskWrite     = "write"     // 工作区写/构建
	RiskNetwork   = "network"   // 联网
	RiskDelete    = "delete"    // 删除
	RiskPrivilege = "privilege" // 提权/改权限
	RiskGitPush   = "git-push"  // 推送/重置/清理
	RiskCritical  = "critical"  // 不可逆的高危命令
)

// criticalPrefixes 直接拒绝的模式（即使 full 权限）。
var criticalPrefixes = []string{
	"rm -rf /", "rm -rf /*", "rm -rf ~", ":(){", "mkfs", "dd if=/dev/zero of=/dev/",
	"shutdown", "reboot", "halt", "diskutil eraseDisk", "chmod -R 777 /",
}

// riskOf 对 argv 做保守分类。分类不决定放行，只决定要不要审批。
func riskOf(argv []string, shell bool) string {
	if len(argv) == 0 {
		return RiskCritical
	}
	joined := strings.Join(argv, " ")
	for _, bad := range criticalPrefixes {
		if strings.Contains(joined, bad) {
			return RiskCritical
		}
	}
	name := filepath.Base(argv[0])
	switch name {
	case "sudo", "su", "doas", "chown", "chmod", "launchctl", "systemctl":
		return RiskPrivilege
	case "rm", "rmdir", "unlink", "truncate":
		return RiskDelete
	case "curl", "wget", "ssh", "scp", "sftp", "nc", "ncat", "telnet", "ftp":
		return RiskNetwork
	case "git":
		if len(argv) > 1 {
			switch argv[1] {
			case "push", "reset", "clean", "rebase", "checkout", "restore", "filter-branch":
				return RiskGitPush
			}
		}
		return RiskWrite
	case "npm", "pnpm", "yarn", "pip", "pip3", "go", "cargo", "make", "cmake", "docker", "kubectl", "helm":
		return RiskWrite
	case "sh", "bash", "zsh", "fish", "python", "python3", "node", "ruby", "perl", "php":
		// 解释器可以把任意代码藏进参数，必须按写类处理，不能靠白名单猜。
		return RiskWrite
	case "ls", "cat", "head", "tail", "grep", "rg", "find", "wc", "file", "stat",
		"which", "pwd", "echo", "sort", "uniq", "diff", "tree", "jq":
		return RiskRead
	}
	if strings.Contains(joined, "|") || strings.Contains(joined, "&&") || strings.Contains(joined, ";") ||
		strings.Contains(joined, ">") || strings.Contains(joined, "<(") {
		return RiskWrite
	}
	if shell {
		return RiskWrite
	}
	return RiskRead
}

// RiskOf 暴露给上层做审批预判。
func RiskOf(argv []string, shell bool) string { return riskOf(argv, shell) }

// IsCritical 判断是否命中不可逆黑名单。
func IsCritical(argv []string, shell bool) bool {
	return riskOf(argv, shell) == RiskCritical
}

// Run 在工作区内执行命令。
//
// 约定：
//   - 以当前用户身份运行；这不是 OS 沙箱，`full` 权限下等同用户本人；
//   - 工作目录默认 root，且必须落在 root 内；
//   - 环境变量只给最小集合，不继承宿主的全部 env（避免密钥泄漏给子进程）；
//   - 超时或取消时杀整个进程组，避免孙进程残留。
func (w *Workspace) Run(ctx context.Context, c Command) (*CommandResult, error) {
	if len(c.Argv) == 0 {
		return nil, errors.New("workspace: empty command")
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = DefaultCommandTimeout
	}
	if timeout > MaxCommandTimeout {
		timeout = MaxCommandTimeout
	}
	cwd := w.Root
	if c.Cwd != "" {
		resolved, err := w.resolve(c.Cwd, true)
		if err != nil {
			return nil, err
		}
		cwd = resolved
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	argv := c.Argv
	if c.Shell {
		argv = []string{"/bin/sh", "-c", strings.Join(c.Argv, " ")}
	}

	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = cwd
	cmd.Env = minimalEnv(w.Root, c.Env)

	var stdout, stderr bytes.Buffer
	out := &cappedWriter{buf: &stdout, max: MaxCommandOutput}
	errOut := &cappedWriter{buf: &stderr, max: MaxCommandOutput}
	cmd.Stdout = out
	cmd.Stderr = errOut
	cmd.Stdin = nil
	cmd.SysProcAttr = procAttr() // 独立进程组，便于整组清理

	start := time.Now()
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("workspace: start %s: %w", argv[0], err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	timedOut := false
	select {
	case <-runCtx.Done():
		timedOut = true
		killGroup(cmd)
		<-done
	case err := <-done:
		if err != nil {
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) {
				return nil, err
			}
		}
	}

	res := &CommandResult{
		Command:    c.display(),
		ExitCode:   cmd.ProcessState.ExitCode(),
		Stdout:     stdout.String(),
		Stderr:     stderr.String(),
		Truncated:  out.truncated || errOut.truncated,
		TimedOut:   timedOut,
		DurationMS: time.Since(start).Milliseconds(),
		Risk:       riskOf(c.Argv, c.Shell),
	}
	if timedOut {
		res.ExitCode = 124
	}
	return res, nil
}

func (c Command) display() string {
	if c.Shell {
		return strings.Join(c.Argv, " ")
	}
	return strings.Join(c.Argv, " ")
}

// minimalEnv 构造最小环境。
//
// HOME 指向工作区内的临时 home，避免命令顺手读到用户真实的 ~/.ssh、~/.aws。
// 需要真实 HOME 的场景由调用方通过 Command.Env 显式覆盖。
func minimalEnv(root string, extra map[string]string) []string {
	home := filepath.Join(os.TempDir(), "dsh-exec-home")
	_ = os.MkdirAll(home, 0o700)
	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + home,
		"TMPDIR=" + os.TempDir(),
		"LANG=en_US.UTF-8",
		"LC_ALL=en_US.UTF-8",
		"TERM=dumb",
		"CI=1",
		"NO_COLOR=1",
		"PWD=" + root,
	}
	// 只透传构建必需、且不含密钥语义的变量。
	for _, k := range []string{"GOPATH", "GOROOT", "GOMODCACHE", "NODE_PATH", "JAVA_HOME", "PNPM_HOME"} {
		if v := os.Getenv(k); v != "" {
			env = append(env, k+"="+v)
		}
	}
	for k, v := range extra {
		if strings.Contains(strings.ToUpper(k), "KEY") || strings.Contains(strings.ToUpper(k), "SECRET") ||
			strings.Contains(strings.ToUpper(k), "TOKEN") {
			continue // 不对子进程透传密钥，即使调用方要求
		}
		env = append(env, k+"="+v)
	}
	return env
}

// cappedWriter 写入上限，超过后丢弃并置位。
type cappedWriter struct {
	mu        sync.Mutex
	buf       *bytes.Buffer
	max       int
	truncated bool
}

func (c *cappedWriter) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	room := c.max - c.buf.Len()
	if room <= 0 {
		c.truncated = true
		return len(p), nil
	}
	if len(p) > room {
		c.buf.Write(p[:room])
		c.truncated = true
		return len(p), nil
	}
	return c.buf.Write(p)
}

//go:build unix

package workspace

import (
	"os/exec"
	"syscall"
)

// procAttr 让命令进入独立进程组，便于整组清理。
func procAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

// killGroup 杀掉整个进程组：只杀 shell 会留下孙进程。
func killGroup(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	_ = cmd.Process.Kill()
}

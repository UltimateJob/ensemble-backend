//go:build windows

package workspace

import (
	"os/exec"
	"syscall"
)

func procAttr() *syscall.SysProcAttr { return nil }

func killGroup(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
}

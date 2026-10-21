//go:build unix

package store

import (
	"fmt"
	"os"
	"syscall"
)

// lockFile 用 flock 给数据目录加独占锁。
// 两个进程同时写同一个目录会互相覆盖，必须挡住。
type lockFile struct {
	f *os.File
}

func acquireLock(path string) (*lockFile, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	// LOCK_NB：抢不到立刻返回，不阻塞
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("数据目录已被另一个实例占用（%s）：%w", path, err)
	}
	// 写入 PID 便于排查
	_ = f.Truncate(0)
	_, _ = f.WriteAt([]byte(fmt.Sprintf("%d\n", os.Getpid())), 0)
	return &lockFile{f: f}, nil
}

func (l *lockFile) release() error {
	if l == nil || l.f == nil {
		return nil
	}
	_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	return l.f.Close()
}

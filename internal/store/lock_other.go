//go:build !unix

package store

// 非 unix 平台暂时不加锁，只做一个存在性提示。
type lockFile struct{}

func acquireLock(path string) (*lockFile, error) { return &lockFile{}, nil }

func (l *lockFile) release() error { return nil }

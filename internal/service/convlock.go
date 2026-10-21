package service

import "sync"

// convLocks 保证同一个会话同时只有一轮编排在跑。
//
// 不加这个的话，两个人几乎同时按回车会各起一个编排器：
// 两边读到的历史都不完整，Agent 的回复交错在一起，
// 而且用户会看到「林回了 A 的上一句、周回了 B 的上一句」这种错位。
type convLocks struct {
	mu sync.Mutex
	m  map[string]chan struct{}
}

func newConvLocks() *convLocks {
	return &convLocks{m: make(map[string]chan struct{})}
}

// TryLock 非阻塞抢锁。抢到返回 true。
func (l *convLocks) TryLock(key string) bool {
	l.mu.Lock()
	ch, ok := l.m[key]
	if !ok {
		ch = make(chan struct{}, 1)
		l.m[key] = ch
	}
	l.mu.Unlock()

	select {
	case ch <- struct{}{}:
		return true
	default:
		return false
	}
}

func (l *convLocks) Unlock(key string) {
	l.mu.Lock()
	ch, ok := l.m[key]
	l.mu.Unlock()
	if !ok {
		return
	}
	select {
	case <-ch:
	default:
	}
}

// Busy 是否有人在说。
func (l *convLocks) Busy(key string) bool {
	l.mu.Lock()
	ch, ok := l.m[key]
	l.mu.Unlock()
	if !ok {
		return false
	}
	return len(ch) > 0
}

package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// sseWriter 串行化 SSE 写端，并在连接空闲时定期发送心跳。
//
// 为什么需要它：
//   - 等首 token 期间（重模型可能十几秒不吐字）连接上没有任何字节，
//     中间的代理（nginx、CDN、LB）一旦认为空闲就会掐断连接，
//     用户看到的就是无限转圈。注释行「: ping」能让链路保持活跃，
//     又会被 EventSource 静默忽略，不打扰前端解析。
//   - 心跳 goroutine 与业务 emit 并发写同一个 ResponseWriter，
//     net/http 不保证并发安全，必须用锁串行化。
type sseWriter struct {
	mu       sync.Mutex
	w        http.ResponseWriter
	flusher  http.Flusher
	canFlush bool

	stopOnce sync.Once
	stopCh   chan struct{} // 关闭后心跳 goroutine 退出
	stopped  chan struct{} // 心跳 goroutine 真正退出后关闭
}

// startSSE 写出 SSE 响应头并启动心跳。
// 调用方必须在连接结束时 defer sw.stop()，否则心跳 goroutine 会泄漏。
func startSSE(w http.ResponseWriter) *sseWriter {
	sw := &sseWriter{
		w:       w,
		stopCh:  make(chan struct{}),
		stopped: make(chan struct{}),
	}
	sw.flusher, sw.canFlush = w.(http.Flusher)

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	go sw.beat()
	return sw
}

// beat 每 15 秒写一行 SSE 注释。
// 15 秒足够短：常见代理的空闲超时多在 30~60 秒；
// 又足够长：不会在日志和带宽上制造噪音。
func (s *sseWriter) beat() {
	defer close(s.stopped)
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-t.C:
			s.mu.Lock()
			if _, err := fmt.Fprint(s.w, ": ping\n\n"); err == nil {
				s.flush()
			}
			s.mu.Unlock()
			// 写失败说明客户端已断开；外层会调用 stop，这里继续等退出信号
		}
	}
}

// writeData 写一条 data: 事件并立即刷出。返回 false 表示连接已写坏。
func (s *sseWriter) writeData(v interface{}) bool {
	b, err := json.Marshal(v)
	if err != nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := fmt.Fprintf(s.w, "data: %s\n\n", b); err != nil {
		return false
	}
	s.flush()
	return true
}

// writeRaw 写一段原始 SSE 文本（如网关直通的 data: [DONE]）。
func (s *sseWriter) writeRaw(line string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := fmt.Fprint(s.w, line); err != nil {
		return false
	}
	s.flush()
	return true
}

// stop 结束心跳并等它真正退出。
// 必须等：handler 返回后再写 ResponseWriter 会 panic，
// 心跳可能正拿着锁在写。
func (s *sseWriter) stop() {
	s.stopOnce.Do(func() { close(s.stopCh) })
	<-s.stopped
}

func (s *sseWriter) flush() {
	if s.canFlush {
		s.flusher.Flush()
	}
}

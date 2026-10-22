package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// 体检要能量出首字延迟和输出速度：先等一会儿再吐 3 个增量，
// 最后给一个 usage。有 usage 时 token 数以 usage 为准。
func TestProbeMeasuresFirstByteAndTokens(t *testing.T) {
	var gotModel string
	var includeUsage bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r) // 余额查询，best-effort
			return
		}
		var req dsRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		gotModel = req.Model
		includeUsage = req.StreamOptions != nil && req.StreamOptions.IncludeUsage

		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		time.Sleep(40 * time.Millisecond)
		for i := 0; i < 3; i++ {
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"字\"}}]}\n\n")
			if flusher != nil {
				flusher.Flush()
			}
			time.Sleep(10 * time.Millisecond)
		}
		fmt.Fprint(w, "data: {\"choices\":[],\"usage\":{\"completion_tokens\":9}}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	d := NewDeepSeek(srv.URL, "test-key", "default-model", 5*time.Second)
	pr := d.Probe(context.Background(), "upstream-x")

	if !pr.OK || pr.HTTPStatus != http.StatusOK {
		t.Fatalf("probe failed: %+v", pr)
	}
	if gotModel != "upstream-x" {
		t.Fatalf("model = %q, want upstream-x", gotModel)
	}
	if !includeUsage {
		t.Fatal("probe should request stream usage")
	}
	if pr.FirstByteMS < 30 {
		t.Fatalf("first byte = %dms, want >= 30ms", pr.FirstByteMS)
	}
	if pr.Chars != 3 {
		t.Fatalf("chars = %d, want 3", pr.Chars)
	}
	if pr.Tokens != 9 {
		t.Fatalf("tokens = %d, want usage 9", pr.Tokens)
	}
	if pr.TokPerSec <= 0 {
		t.Fatalf("tok/s = %v, want > 0", pr.TokPerSec)
	}
}

// 上游不吐 usage 时，按增量数估算 token。
func TestProbeEstimatesTokensWithoutUsage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for i := 0; i < 4; i++ {
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\n")
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	d := NewDeepSeek(srv.URL, "test-key", "default-model", 5*time.Second)
	pr := d.Probe(context.Background(), "")

	if !pr.OK {
		t.Fatalf("probe failed: %+v", pr)
	}
	if pr.Tokens != 4 {
		t.Fatalf("tokens = %d, want 4 (estimated from deltas)", pr.Tokens)
	}
	if pr.Model != "default-model" {
		t.Fatalf("empty model should fall back to provider default, got %q", pr.Model)
	}
}

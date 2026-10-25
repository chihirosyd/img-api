package middleware

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"img-api/internal/logger"
)

// TestMain 集中初始化测试日志（替代 init：显式生命周期，避免与
// 其他并行测试包共享全局 logger 的隐式耦合）。
func TestMain(m *testing.M) {
	logger.L = slog.New(slog.NewTextHandler(io.Discard, nil))
	os.Exit(m.Run())
}

// TestRecoveryReturnsJSON 回归 Z11：panic 恢复后的响应必须是 JSON 契约
// （此前 http.Error 输出 text/plain + 换行，与 API 契约不一致）。
func TestRecoveryReturnsJSON(t *testing.T) {
	h := Recovery()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))

	r := httptest.NewRequest("GET", "/random", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	if !strings.Contains(w.Body.String(), `"code":500`) {
		t.Fatalf("body = %q, want JSON code 500", w.Body.String())
	}
}

// TestRecoveryKeepsPartialResponse 响应已提交后 panic：保持已写出的内容，
// 不再追加第二个 body（500 写不进去的场景不能拼出两个响应体）。
func TestRecoveryKeepsPartialResponse(t *testing.T) {
	h := Recovery()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("partial"))
		panic("boom after write")
	}))

	r := httptest.NewRequest("GET", "/random", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if got := w.Body.String(); got != "partial" {
		t.Fatalf("body = %q, want only the partial content", got)
	}
}

// TestRecoveryRethrowsErrAbortHandler http.ErrAbortHandler 是主动断连信号，
// 必须原样重新抛出交由 net/http 处理，不能被改写成 500。
func TestRecoveryRethrowsErrAbortHandler(t *testing.T) {
	h := Recovery()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic(http.ErrAbortHandler)
	}))

	defer func() {
		if rec := recover(); rec != http.ErrAbortHandler {
			t.Fatalf("recover = %v, want http.ErrAbortHandler rethrown", rec)
		}
	}()

	r := httptest.NewRequest("GET", "/random", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
}

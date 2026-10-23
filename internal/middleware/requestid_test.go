package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestRequestID(t *testing.T) {
	router := http.NewServeMux()
	router.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	wrapped := RequestID()(router)

	// 无客户端 ID → 自动生成 16 位十六进制
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if rid := w.Header().Get("X-Request-ID"); len(rid) != 16 {
		t.Fatalf("generated id = %q, want 16 chars", rid)
	}

	// 客户端 ID → 复用
	w = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", "trace-123")
	wrapped.ServeHTTP(w, req)
	if got := w.Header().Get("X-Request-ID"); got != "trace-123" {
		t.Fatalf("client id not reused: %q", got)
	}

	// 超长输入 → 截断到 64 rune 且不切断多字节 UTF-8 字符
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", strings.Repeat("中", 100))
	wrapped.ServeHTTP(w, req)
	got := w.Header().Get("X-Request-ID")
	if !utf8.ValidString(got) {
		t.Fatalf("truncated id is not valid utf-8: %q", got)
	}
	if n := len([]rune(got)); n != 64 {
		t.Fatalf("truncated rune length = %d, want 64", n)
	}

	// 回归 R5-02：字节超限但字符数不足 64（22 个中文 = 66 字节 / 22 rune）。
	// 此前 len() 按字节判断、runes[:64] 按字符切片，这里会越界 panic。
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	narrow := strings.Repeat("中", 22)
	req.Header.Set("X-Request-ID", narrow)
	wrapped.ServeHTTP(w, req)
	if got := w.Header().Get("X-Request-ID"); got != narrow {
		t.Fatalf("byte-over-rune-under id = %q, want %q（不该截断，更不该 panic）", got, narrow)
	}

	// 4 字节字符（emoji）截断后仍必须是合法 UTF-8 且正好 64 个字符
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", strings.Repeat("😀", 70))
	wrapped.ServeHTTP(w, req)
	got = w.Header().Get("X-Request-ID")
	if !utf8.ValidString(got) {
		t.Fatalf("emoji id is not valid utf-8: %q", got)
	}
	if n := len([]rune(got)); n != 64 {
		t.Fatalf("emoji truncated rune length = %d, want 64", n)
	}

	// 含控制字符的客户端 ID → 重新生成（防御性清洗：该值会回写响应头）
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", "bad\x01id")
	wrapped.ServeHTTP(w, req)
	if got := w.Header().Get("X-Request-ID"); got == "bad\x01id" || len(got) != 16 {
		t.Fatalf("control-char id should be regenerated, got %q", got)
	}
}

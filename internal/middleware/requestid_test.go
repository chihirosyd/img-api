package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
)

func TestRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(RequestID())
	router.GET("/", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	// 无客户端 ID → 自动生成 16 位十六进制
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if rid := w.Header().Get("X-Request-ID"); len(rid) != 16 {
		t.Fatalf("generated id = %q, want 16 chars", rid)
	}

	// 客户端 ID → 复用
	w = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", "trace-123")
	router.ServeHTTP(w, req)
	if got := w.Header().Get("X-Request-ID"); got != "trace-123" {
		t.Fatalf("client id not reused: %q", got)
	}

	// 超长输入 → 截断到 64 rune 且不切断多字节 UTF-8 字符
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", strings.Repeat("中", 100))
	router.ServeHTTP(w, req)
	got := w.Header().Get("X-Request-ID")
	if !utf8.ValidString(got) {
		t.Fatalf("truncated id is not valid utf-8: %q", got)
	}
	if n := len([]rune(got)); n != 64 {
		t.Fatalf("truncated rune length = %d, want 64", n)
	}
}

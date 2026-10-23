package handler

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// roundTripFunc 让测试用一个函数冒充 HTTP 传输层，
// 从而在不监听端口、不发真实请求的前提下构造任意上游响应。
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// errReader 第一次 Read 就返回错误，模拟上游传输中途断开（RST）。
type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("connection reset by peer") }

// withFakeProxy 替换全局 proxyClient 并返回恢复函数。
func withFakeProxy(t *testing.T, status int, contentType string, body io.Reader) {
	t.Helper()
	old := proxyClient
	t.Cleanup(func() { proxyClient = old })
	proxyClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: status,
			Header:     http.Header{"Content-Type": []string{contentType}},
			Body:       io.NopCloser(body),
		}, nil
	})}
}

// TestProxyImageRejectsNon2xx 回归 R5-05：上游 404/5xx 即使返回 image/* 也
// 必须按失败处理，不能当 200 转发（客户端会拿到"错误占位图"且统计记成功）。
func TestProxyImageRejectsNon2xx(t *testing.T) {
	withFakeProxy(t, http.StatusNotFound, "image/png", strings.NewReader("not found"))
	h := &APIHandler{}
	rec := httptest.NewRecorder()

	if ok := h.proxyImage(rec, httptest.NewRequest(http.MethodGet, "/", nil), "http://upstream.test/x.png"); ok {
		t.Fatal("proxyImage = true, want false（非 2xx 必须失败）")
	}
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "404") {
		t.Errorf("body = %q, want 含上游状态码 404", body)
	}
}

// TestProxyImageCopyErrorNotSuccess 回归 R5-06：传输中途断开时不能返回 true，
// 否则调用方会把半张图记成成功。
func TestProxyImageCopyErrorNotSuccess(t *testing.T) {
	withFakeProxy(t, http.StatusOK, "image/png", errReader{})
	h := &APIHandler{}
	rec := httptest.NewRecorder()

	if ok := h.proxyImage(rec, httptest.NewRequest(http.MethodGet, "/", nil), "http://upstream.test/x.png"); ok {
		t.Fatal("proxyImage = true, want false（复制出错不得记成功）")
	}
}

// TestProxyImageAbortsWhenExceedingLimit 回归 R5-07：超过上限时必须以
// http.ErrAbortHandler 中止响应（net/http 会静默断连），而不是正常收尾让
// 客户端把截断图当完整图片。测试把上限调到 8 字节以免真的写 50MB。
func TestProxyImageAbortsWhenExceedingLimit(t *testing.T) {
	oldMax := maxProxyImageBytes
	t.Cleanup(func() { maxProxyImageBytes = oldMax })
	maxProxyImageBytes = 8

	withFakeProxy(t, http.StatusOK, "image/png", strings.NewReader("0123456789abcdefghij")) // 20 字节
	h := &APIHandler{}

	panicked := false
	func() {
		defer func() {
			panicked = true
			if rec := recover(); rec != http.ErrAbortHandler {
				t.Fatalf("recover = %v, want http.ErrAbortHandler", rec)
			}
		}()
		_ = h.proxyImage(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil), "http://upstream.test/x.png")
	}()
	if !panicked {
		t.Fatal("proxyImage 未中止响应（应 panic http.ErrAbortHandler）")
	}
}

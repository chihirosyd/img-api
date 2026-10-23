package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// readerOnly 只暴露 Read：屏蔽底层 Reader 可能实现的 io.WriterTo。
//
// 这一点很重要：io.Copy 的接口选择顺序是 dst.(ReaderFrom) → src.(WriterTo) → 缓冲循环
// （见 io.copyBuffer）。strings.Reader / bytes.Reader / bytes.Buffer 都实现了
// WriterTo，用它们做源时 io.Copy 会直接走 WriterTo，永远不碰 dst 的 ReadFrom——
// 于是"包装器是否透传 ReadFrom"根本测不出来。
// 生产代码的两个源都不实现 WriterTo：*io.LimitedReader（proxyImage）与 *os.File（serveLocalFile）。
type readerOnly struct{ r io.Reader }

func (o readerOnly) Read(p []byte) (int, error) { return o.r.Read(p) }

// readFromRecorder 是实现了 io.ReaderFrom 的 ResponseWriter，
// 用于验证中间件包装器是否把 ReadFrom 快速路径透传下去（回归 P5-1）。
type readFromRecorder struct {
	*httptest.ResponseRecorder
	readFromCalled bool
}

func (r *readFromRecorder) ReadFrom(src io.Reader) (int64, error) {
	r.readFromCalled = true
	return io.Copy(r.ResponseRecorder, src)
}

// TestStatusRecorderForwardsReadFrom 回归 P5-1：
// accesslog 的 statusRecorder 必须透传 io.ReaderFrom，否则 handler 的
// io.Copy(w, resp.Body / *os.File) 会退化为 32KB 用户态缓冲拷贝，
// 拿不到 net.TCPConn 的 sendfile 快速路径。
func TestStatusRecorderForwardsReadFrom(t *testing.T) {
	rec := &readFromRecorder{ResponseRecorder: httptest.NewRecorder()}
	sr := &statusRecorder{ResponseWriter: rec, status: http.StatusOK}

	const body = "image-bytes-payload"
	n, err := io.Copy(sr, readerOnly{strings.NewReader(body)})
	if err != nil {
		t.Fatalf("io.Copy: %v", err)
	}
	if n != int64(len(body)) {
		t.Fatalf("copied %d bytes, want %d", n, len(body))
	}
	if !rec.readFromCalled {
		t.Error("底层 ReadFrom 未被调用：快速路径被包装器截断")
	}
	if rec.Body.String() != body {
		t.Errorf("body = %q, want %q", rec.Body.String(), body)
	}
	// 隐式 200：ReadFrom 不经过 WriteHeader，状态码应保持初始值
	if sr.status != http.StatusOK {
		t.Errorf("记录状态码 = %d, want 200", sr.status)
	}
}

// TestRecoveryWriterForwardsReadFrom 同上，验证 recovery 的包装器。
func TestRecoveryWriterForwardsReadFrom(t *testing.T) {
	rec := &readFromRecorder{ResponseRecorder: httptest.NewRecorder()}
	rw := &recoveryWriter{ResponseWriter: rec}

	const body = "another-payload"
	if _, err := io.Copy(rw, readerOnly{strings.NewReader(body)}); err != nil {
		t.Fatalf("io.Copy: %v", err)
	}
	if !rec.readFromCalled {
		t.Error("底层 ReadFrom 未被调用：快速路径被包装器截断")
	}
	if rec.Body.String() != body {
		t.Errorf("body = %q, want %q", rec.Body.String(), body)
	}
	// ReadFrom 不经过 Write/WriteHeader，必须自己标记已提交，
	// 否则 panic 恢复时会把 500 追加到已写出的响应体之后（拼接两个 body）
	if !rw.wroteHeader {
		t.Error("wroteHeader 未置位：panic 恢复会追加第二个 body")
	}
}

// TestRestoredWriterSkipsReadFrom 反向断言：去掉 ReadFrom 透传后快速路径确实断掉。
//
// 用于固化 P5-1 的实证结论（也说明本测试组真的在测这条路径，而不是恒真）。
func TestRestoredWriterSkipsReadFrom(t *testing.T) {
	rec := &readFromRecorder{ResponseRecorder: httptest.NewRecorder()}
	// writerOnly 隐藏 ResponseWriter 的 ReadFrom，模拟"未实现透传"的包装器
	w := writerOnly{rec}

	if _, err := io.Copy(w, readerOnly{strings.NewReader("payload")}); err != nil {
		t.Fatalf("io.Copy: %v", err)
	}
	if rec.readFromCalled {
		t.Error("隐藏 ReadFrom 后仍被调用，说明断言前提不成立")
	}
	if rec.Body.String() != "payload" {
		t.Errorf("body = %q", rec.Body.String())
	}
}

// writerOnly 只暴露 Write，用于屏蔽底层可能实现的 ReadFrom。
type writerOnly struct{ w io.Writer }

func (o writerOnly) Write(p []byte) (int, error) { return o.w.Write(p) }

// TestWrappersFallbackWhenNoReadFrom 底层不支持 ReadFrom 时，
// 包装器不得死循环，且内容必须正确写出。
func TestWrappersFallbackWhenNoReadFrom(t *testing.T) {
	const body = "plain-recorder-payload"

	t.Run("statusRecorder", func(t *testing.T) {
		rec := httptest.NewRecorder() // 原生 ResponseRecorder 无 ReadFrom
		sr := &statusRecorder{ResponseWriter: rec, status: http.StatusOK}
		n, err := io.Copy(sr, readerOnly{strings.NewReader(body)})
		if err != nil || n != int64(len(body)) {
			t.Fatalf("io.Copy n=%d err=%v", n, err)
		}
		if rec.Body.String() != body {
			t.Errorf("body = %q, want %q", rec.Body.String(), body)
		}
	})

	t.Run("recoveryWriter", func(t *testing.T) {
		rec := httptest.NewRecorder()
		rw := &recoveryWriter{ResponseWriter: rec}
		n, err := io.Copy(rw, readerOnly{strings.NewReader(body)})
		if err != nil || n != int64(len(body)) {
			t.Fatalf("io.Copy n=%d err=%v", n, err)
		}
		if rec.Body.String() != body {
			t.Errorf("body = %q, want %q", rec.Body.String(), body)
		}
		if !rw.wroteHeader {
			t.Error("wroteHeader 未置位")
		}
	})
}

// TestTruncateQuery 访问日志的 query 截断（回归 P5-2）。
func TestTruncateQuery(t *testing.T) {
	short := "source=txt&category=anime"
	if got := truncateQuery(short); got != short {
		t.Errorf("短 query 不应改动：%q", got)
	}

	long := strings.Repeat("a", maxLoggedQueryLen+50)
	got := truncateQuery(long)
	if len(got) <= maxLoggedQueryLen {
		t.Errorf("超长 query 应带截断标记：len=%d", len(got))
	}
	if !strings.HasSuffix(got, "...(truncated)") {
		t.Errorf("缺少截断标记：%q", got[len(got)-20:])
	}
	if !strings.HasPrefix(got, strings.Repeat("a", maxLoggedQueryLen)) {
		t.Error("截断前缀不正确")
	}
}

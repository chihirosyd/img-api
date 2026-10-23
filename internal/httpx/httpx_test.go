package httpx

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"img-api/internal/logger"
)

// TestMain 初始化全局 logger（WriteJSON 失败时写告警日志），输出丢弃避免噪声。
// 必须 os.Exit(m.Run())：否则测试失败不会反映到进程退出码上。
func TestMain(m *testing.M) {
	logger.L = slog.New(slog.NewTextHandler(io.Discard, nil))
	os.Exit(m.Run())
}

// TestWriteJSONFallbackOnMarshalError 回归 D10 / P4-5：
// 响应体无法序列化时，不得返回「200 + 空 body + JSON Content-Type」。
// 契约：先序列化再写响应头；失败记日志并降级为 500 JSON。
func TestWriteJSONFallbackOnMarshalError(t *testing.T) {
	w := httptest.NewRecorder()

	ok := WriteJSON(w, http.StatusOK, map[string]any{"unsupported": make(chan int)})
	if ok {
		t.Error("序列化失败时 WriteJSON 应返回 false")
	}

	res := w.Result()
	if res.StatusCode != http.StatusInternalServerError {
		t.Errorf("状态码 = %d, want 500（不得残留 200）", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	body := w.Body.Bytes()
	if len(body) == 0 {
		t.Fatal("body 为空：客户端会拿到无法解析的空响应")
	}
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("降级响应不是合法 JSON: %v (body=%q)", err, body)
	}
	if decoded["code"] != float64(500) {
		t.Errorf("降级 body = %v, want code=500", decoded)
	}
}

// TestWriteJSONSuccess 正常路径：状态码、Content-Type、body 均为合法 JSON。
func TestWriteJSONSuccess(t *testing.T) {
	w := httptest.NewRecorder()

	if ok := WriteJSON(w, http.StatusNotFound, map[string]any{"code": 404, "message": "nope"}); !ok {
		t.Error("正常序列化应返回 true")
	}
	if w.Code != http.StatusNotFound {
		t.Errorf("状态码 = %d, want 404", w.Code)
	}
	var decoded map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("body 不是合法 JSON: %v (body=%q)", err, w.Body.String())
	}
	if decoded["message"] != "nope" {
		t.Errorf("body = %v", decoded)
	}
}

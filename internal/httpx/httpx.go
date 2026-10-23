// Package httpx 提供 handler 与 middleware 共用的 HTTP 输出工具。
package httpx

import (
	"encoding/json"
	"net/http"

	"img-api/internal/logger"
)

// WriteJSON 输出 JSON 响应；返回是否成功写出。
//
// 契约：先序列化、再写响应头。此前是 WriteHeader(code) 之后才 Encode，一旦编组失败
// （响应含 chan/func/NaN 等不可序列化字段）客户端会拿到「200 + 空 body + JSON
// Content-Type」，且错误被丢弃、无任何日志，排障时无从下手。现在失败会：
//   - 记 Warn 日志（不中断服务，便于发现编码 bug）
//   - 降级为 500 JSON 错误响应（若尚未写出任何内容）
func WriteJSON(w http.ResponseWriter, code int, data any) bool {
	body, err := json.Marshal(data)
	if err != nil {
		logger.L.Warn("write json failed: response body is not serializable",
			"status", code, "error", err)
		writeJSONFallback(w)
		return false
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_, _ = w.Write(body)
	_, _ = w.Write([]byte("\n"))
	return true
}

// writeJSONFallback 在响应体无法序列化时输出固定的 500 JSON。
// 该错误信息是静态字面量，序列化必然成功。
func writeJSONFallback(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusInternalServerError)
	_, _ = w.Write([]byte(`{"code":500,"message":"internal server error"}` + "\n"))
}

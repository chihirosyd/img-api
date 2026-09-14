// Package httpx 提供 handler 与 middleware 共用的 HTTP 输出工具。
package httpx

import (
	"encoding/json"
	"net/http"
)

// WriteJSON 输出 JSON 响应（Content-Type 与状态码）。
// handler 与 middleware 共用同一实现，保证响应契约一致。
func WriteJSON(w http.ResponseWriter, code int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(data)
}

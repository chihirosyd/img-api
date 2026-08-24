package handler

import (
	"encoding/json"
	"net/http"
)

// writeJSON 输出 JSON 响应（Content-Type 与状态码）。
func writeJSON(w http.ResponseWriter, code int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(data)
}

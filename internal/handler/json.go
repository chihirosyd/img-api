package handler

import (
	"img-api/internal/httpx"
)

// writeJSON 是共享实现（internal/httpx）的包内别名，
// 与 middleware 层共用同一实现，保证响应契约一致。
var writeJSON = httpx.WriteJSON

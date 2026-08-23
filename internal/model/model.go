// Package model 定义项目核心数据结构。
//
// 包含：
//   - 枚举类型（设备类型 / 图片来源 / 返回模式）
//   - 请求/响应结构体
//   - 共享常量（图片扩展名等）
//
// 业务错误类型见 errors.go。
package model

// ──────────────────────────────────────────────
// 共享常量
// ──────────────────────────────────────────────

// ImageExts 定义支持的图片文件扩展名（小写）。
// 供 LocalRepository 索引扫描、build-index 命令、API 代理输出等模块共用。
var ImageExts = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true,
	".gif": true, ".webp": true, ".bmp": true, ".svg": true,
}

// ImageMimeTypes 是图片扩展名 → 标准 MIME 类型的映射，与 ImageExts 一一对应。
// 供 serveLocalFile 输出 Content-Type 使用；集中定义避免与 ImageExts 两处清单不一致。
// 注意 .jpg 的标准 MIME 是 image/jpeg（"image/jpg" 并非规范值，部分严格客户端会拒绝）。
var ImageMimeTypes = map[string]string{
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
	".gif":  "image/gif",
	".webp": "image/webp",
	".bmp":  "image/bmp",
	".svg":  "image/svg+xml",
}

// ──────────────────────────────────────────────
// 设备类型（type 参数）
// ──────────────────────────────────────────────

// DeviceType 表示客户端设备类型。
// 支持显式指定（pc/pe）或自动检测（auto）。
type DeviceType string

const (
	DeviceAuto DeviceType = "auto" // 通过 User-Agent 自动判断
	DevicePC   DeviceType = "pc"   // 强制返回 PC（横屏）图片
	DevicePE   DeviceType = "pe"   // 强制返回手机（竖屏）图片
)

// Valid 返回设备类型是否为合法枚举值。
func (d DeviceType) Valid() bool {
	switch d {
	case DeviceAuto, DevicePC, DevicePE:
		return true
	}
	return false
}

// ──────────────────────────────────────────────
// 图片来源（source 参数）
// ──────────────────────────────────────────────

// SourceType 表示图片来源类型。
// 不同来源对应不同的 Repository 实现。
type SourceType string

const (
	SourceTxt      SourceType = "txt"      // TXT 文件图库（一行一个 URL）
	SourceLocal    SourceType = "local"    // 本地磁盘图片文件
	SourceExternal SourceType = "external" // 远端 API 图片池
)

// Valid 返回来源类型是否为合法枚举值。
func (s SourceType) Valid() bool {
	switch s {
	case SourceTxt, SourceLocal, SourceExternal:
		return true
	}
	return false
}

// ──────────────────────────────────────────────
// 返回模式（mode 参数）
// ──────────────────────────────────────────────

// Mode 表示 API 的响应方式。
type Mode string

const (
	ModeRedirect Mode = "redirect" // HTTP 302 重定向到图片真实 URL
	ModeJSON     Mode = "json"     // 返回 JSON 对象（含 URL、分类等元数据）
	ModeImage    Mode = "image"    // 代理模式：服务端拉取图片后直接输出二进制
)

// Valid 返回返回模式是否为合法枚举值。
func (m Mode) Valid() bool {
	switch m {
	case ModeRedirect, ModeJSON, ModeImage:
		return true
	}
	return false
}

// ──────────────────────────────────────────────
// 请求参数
// ──────────────────────────────────────────────

// RandomParams 封装 GET /random 的全部查询参数。
// Category 支持逗号分隔多选（如 "anime,scenery"），
// Service 层会从中随机选取一个。
type RandomParams struct {
	Type     DeviceType // 设备类型（auto/pc/pe）
	Source   SourceType // 图片来源（txt/local/external）
	Mode     Mode       // 返回模式（redirect/json/image）
	Category string     // 分类名（逗号分隔多选，如 "anime,scenery"）
	APIName  string     // 外部 API 名称（source=external 时可用，空=随机选取）
}

// DefaultRandomParams 返回一组安全的默认参数。
// 未传参的请求将使用此默认值。
func DefaultRandomParams() RandomParams {
	return RandomParams{
		Type:     DeviceAuto,
		Source:   SourceTxt,
		Mode:     ModeRedirect,
		Category: "default",
	}
}

// ──────────────────────────────────────────────
// 图片条目
// ──────────────────────────────────────────────

// Image 表示仓库返回的一张图片。
// 会被序列化为 JSON（mode=json）或被重定向（mode=redirect）。
type Image struct {
	URL      string `json:"url"`      // 图片的绝对 URL 或本地文件路径
	Category string `json:"category"` // 所属分类（方便前端按分类展示）
	Width    int    `json:"width"`    // 图片宽度（px），未知时为 0
	Height   int    `json:"height"`   // 图片高度（px），未知时为 0
}

// ──────────────────────────────────────────────
// API 响应体
// ──────────────────────────────────────────────

// RandomResponse 是 mode=json 时的响应结构。
// Data 使用 omitempty：出错时不展示 data 字段。
type RandomResponse struct {
	Code    int    `json:"code"`             // 业务状态码（200=成功）
	Message string `json:"message"`          // 人类可读的消息
	Data    *Image `json:"data,omitempty"`   // 图片元数据
}

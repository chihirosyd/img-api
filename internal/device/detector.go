// Package device 负责检测客户端设备类型（PC 还是手机）。
//
// 通过解析 HTTP User-Agent 请求头实现，
// 不依赖任何外部 API 或第三方库，纯字符串匹配。
//
// 检测逻辑：如果 UA 包含任意移动设备关键词 → 判定为 PE（手机），
// 否则为 PC（桌面）。匹配时忽略大小写。
package device

import (
	"strings"

	"img-api/internal/model"
)

// mobileKeywords 是常见移动设备 User-Agent 中会出现的特征词。
// 涵盖 iOS、Android、Windows Phone 等主流平台的设备标识。
var mobileKeywords = []string{
	"Mobile", "Android", "iPhone", "iPad", "iPod",
	"BlackBerry", "Windows Phone", "Opera Mini", "IEMobile",
	"webOS", "Kindle", "Silk", "PlayBook",
}

// Detect 根据 HTTP User-Agent 字符串判断设备类型（大小写不敏感）。
//
// 返回值不会是 DeviceAuto——要么是 DevicePC 要么是 DevicePE。
// 不支持平板/桌面精细区分，统一按二分类处理。
func Detect(userAgent string) model.DeviceType {
	ua := strings.ToLower(userAgent)
	for _, kw := range mobileKeywords {
		if strings.Contains(ua, strings.ToLower(kw)) {
			return model.DevicePE
		}
	}
	return model.DevicePC
}

// Resolve 综合用户指定的 type 参数和 User-Agent，决定最终设备类型。
//
// 优先级：
//  1. 用户显式指定 pc 或 pe → 直接使用
//  2. type=auto 或参数为空 → 通过 UA 自动检测
//  3. UA 异常（空字符串等） → 默认返回 PC
func Resolve(userType model.DeviceType, userAgent string) model.DeviceType {
	if userType == model.DevicePE || userType == model.DevicePC {
		return userType
	}
	return Detect(userAgent)
}

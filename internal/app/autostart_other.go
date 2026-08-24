//go:build !windows

package app

import "fmt"

// SetAutoStart 非 Windows 平台的空实现：开机自启仅 Windows GUI 支持。
func SetAutoStart(enabled bool, exePath, args string) error {
	return fmt.Errorf("auto start is only supported on windows")
}

//go:build windows

package app

import (
	"fmt"

	"golang.org/x/sys/windows/registry"
)

// autostartRunKey 是 Windows 当前用户的开机自启注册表位置（无需管理员权限）。
const (
	autostartRunKey = `Software\Microsoft\Windows\CurrentVersion\Run`
	autostartName   = "img-api-gui"
)

// SetAutoStart 设置/取消 img-api-gui 的开机自启（当前用户级）。
// exePath 为 GUI 可执行文件路径；args 为附加参数（如 --background）。
func SetAutoStart(enabled bool, exePath, args string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, autostartRunKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("open run key: %w", err)
	}
	defer k.Close()

	if !enabled {
		if err := k.DeleteValue(autostartName); err != nil && err != registry.ErrNotExist {
			return fmt.Errorf("delete run value: %w", err)
		}
		return nil
	}

	cmd := `"` + exePath + `"`
	if args != "" {
		cmd += " " + args
	}
	if err := k.SetStringValue(autostartName, cmd); err != nil {
		return fmt.Errorf("set run value: %w", err)
	}
	return nil
}

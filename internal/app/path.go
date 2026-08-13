// Package app 提供跨 cmd 工具共享的基础设施。
package app

import (
	"os"
	"path/filepath"
)

// RootPath 定位项目根目录。
//
// 优先级：APP_ROOT 环境变量 → 可执行文件目录 → 当前工作目录
func RootPath() string {
	if root := os.Getenv("APP_ROOT"); root != "" {
		return root
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
	}
	wd, _ := os.Getwd()
	return wd
}

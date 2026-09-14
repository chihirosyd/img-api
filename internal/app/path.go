// Package app 提供跨 cmd 工具共享的基础设施。
package app

import (
	"os"
	"path/filepath"
)

// RootPath 定位项目根目录。
//
// 优先级：APP_ROOT 环境变量 → 可执行文件目录 → 当前工作目录。
//
// 可执行文件目录以 resources/ 目录作为识别标记：该目录在源码环境与
// 发布包（zip / Docker 镜像）中都存在。此前用 go.mod 标记，而发布包
// 不含 go.mod，导致 GUI 开机自启（工作目录为 System32）等场景无法命中，
// 服务从错误根目录启动甚至启动失败。
func RootPath() string {
	if root := os.Getenv("APP_ROOT"); root != "" {
		return root
	}
	if exe, err := os.Executable(); err == nil {
		if dir := rootFromExeDir(filepath.Dir(exe)); dir != "" {
			return dir
		}
	}
	wd, _ := os.Getwd()
	return wd
}

// rootFromExeDir 判断目录是否为项目根目录（以 resources/ 目录为标记）。
// 是则返回该目录，否则返回空字符串。
func rootFromExeDir(dir string) string {
	if fi, err := os.Stat(filepath.Join(dir, "resources")); err == nil && fi.IsDir() {
		return dir
	}
	return ""
}

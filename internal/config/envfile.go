// Package config 的 .env 文件读写工具。
//
// 供 GUI 控制面板使用：图形界面编辑 .env 时，
// 保留注释与无关行，只替换/追加涉及的键值。
package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// EnvFile 返回项目根目录下 .env 文件的路径。
func EnvFile(rootPath string) string {
	return filepath.Join(rootPath, ".env")
}

// ReadEnvValues 读取 .env 中的全部键值对（不解析 $/引号，保留原样）。
// 文件不存在时返回空 map（不报错，GUI 首次运行尚无 .env）。
func ReadEnvValues(rootPath string) (map[string]string, error) {
	values := make(map[string]string)

	f, err := os.Open(EnvFile(rootPath))
	if err != nil {
		if os.IsNotExist(err) {
			return values, nil
		}
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// 只处理简单键值（带 # 内联注释的会被截断，与 dotenv 解析行为一致）
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		values[strings.TrimSpace(key)] = strings.TrimSpace(val)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return values, nil
}

// UpdateEnvFile 更新 .env 中的键值：已存在的键替换值行，不存在的追加到末尾；
// 注释与其他行原样保留。文件不存在时创建（0755 目录权限由调用方保证）。
func UpdateEnvFile(rootPath string, updates map[string]string) error {
	path := EnvFile(rootPath)

	lines, err := readLines(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	applied := make(map[string]bool, len(updates))

	// 已存在的键：替换值
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		key, _, ok := strings.Cut(trimmed, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if newVal, found := updates[key]; found {
			lines[i] = key + "=" + newVal
			applied[key] = true
		}
	}

	// 不存在的键：追加到末尾（前插一个空行分隔）
	var appended []string
	for key, val := range updates {
		if !applied[key] {
			appended = append(appended, key+"="+val)
		}
	}
	if len(appended) > 0 {
		if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
			lines = append(lines, "")
		}
		lines = append(lines, appended...)
	}

	if err := writeLines(path, lines); err != nil {
		return fmt.Errorf("write .env: %w", err)
	}
	return nil
}

// readLines 读取文件全部行（文件不存在返回 nil, os.ErrNotExist 错误）。
func readLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}

// writeLines 原子写入全部行（先写临时文件再重命名，避免中途崩溃损坏配置）。
func writeLines(path string, lines []string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	data := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(tmp, []byte(data), 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

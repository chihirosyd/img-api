// Package config 的 .env 文件读写工具。
//
// 供 GUI 控制面板使用：图形界面编辑 .env 时，
// 保留注释与无关行，只替换/追加涉及的键值。
package config

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// EnvFile 返回项目根目录下 .env 文件的路径。
func EnvFile(rootPath string) string {
	return filepath.Join(rootPath, ".env")
}

// ReadEnvValues 读取 .env 中的全部键值对（不解析 $/引号，保留原样，
// 但会剥离"行内注释"，与 godotenv 的解析结果保持一致）。
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
	first := true
	for scanner.Scan() {
		line := scanner.Text()
		if first {
			line = strings.TrimPrefix(line, "\ufeff") // UTF-8 BOM
			first = false
		}
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		// 识别 shell 风格的 export 前缀：export APP_PORT=9090 的键是 APP_PORT
		if lower := strings.ToLower(key); strings.HasPrefix(lower, "export ") {
			key = strings.TrimSpace(key[len("export "):])
		}
		values[key] = stripInlineComment(strings.TrimSpace(val))
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return values, nil
}

// stripInlineComment 剥离行内注释，并去掉成对引号（与 godotenv 的解析结果保持一致）：
//
//	"30   # 保留天数"  → "30"
//	`"true"`           → "true"     （godotenv 会剥掉引号，GUI 也必须剥）
//	`'pa#ss'`          → "pa#ss"    （引号内的 # 是字面量）
//	"pa#ss"            → "pa"       （无空格分隔时 godotenv 同样截断）
//
// 此前以引号开头的值整体原样返回：`.env` 里写 RATE_LIMIT_ENABLED="true" 时
// GUI 拿到带引号的字符串，与 "true" 比较为假 → 复选框显示未勾选，
// 用户点保存就把限流真的写成 false；APP_PORT="8080" 则让 Atoi 失败、整页存不了。
func stripInlineComment(val string) string {
	if val != "" && (val[0] == '\'' || val[0] == '"') {
		quote := val[0]
		// 找最后一个同类引号作为闭合引号：容忍引号后跟行内注释
		if end := strings.LastIndexByte(val, quote); end > 0 {
			return val[1:end]
		}
		return val
	}
	if i := strings.IndexByte(val, '#'); i >= 0 {
		return strings.TrimSpace(val[:i])
	}
	return val
}

// quoteEnvValue 在必要时给写回的值加单引号。
//
// 含 `#` 或 `$` 的值不加引号会被当成行内注释 / 变量展开，写回后值就变样了，
// 甚至让整份 .env 解析失败（而 GUI 仍提示保存成功）。单引号内二者都是字面量。
// 值本身含单引号时保持原样（dotenv 单引号内无法转义）。
func quoteEnvValue(v string) string {
	if v == "" || strings.Contains(v, "'") {
		return v
	}
	if strings.ContainsAny(v, "#$") || strings.TrimSpace(v) != v {
		return "'" + v + "'"
	}
	return v
}

// UpdateEnvFile 更新 .env 中的键值：已存在的键替换值行（键匹配大小写不敏感），
// 不存在的追加到末尾；注释与其他行原样保留。文件不存在时创建（0755 目录权限由调用方保证）。
// 写入时保留原文件的行尾风格（CRLF/LF），避免 Windows 记事本出现混合行尾。
func UpdateEnvFile(rootPath string, updates map[string]string) error {
	path := EnvFile(rootPath)

	lines, err := readLines(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	// 检测原文件行尾风格（默认 LF）
	eol := "\n"
	if b, rerr := os.ReadFile(path); rerr == nil && bytes.Contains(b, []byte("\r\n")) {
		eol = "\r\n"
	}

	// 大小写不敏感的更新表：.env 中 app_port 与 APP_PORT 是同一个键，
	// 替换时保留原键名写法，避免追加重复键
	updatesFold := make(map[string]string, len(updates))
	for k, v := range updates {
		updatesFold[strings.ToLower(k)] = v
	}
	applied := make(map[string]bool, len(updates))

	// 已存在的键：替换值（保留原键名写法）
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
		// 保留 `export ` 前缀：写成 export APP_PORT=9090 时，键名实际是 APP_PORT，
		// 此前不识别前缀会把整串当成键，替换不到就再追加一行 → 文件出现重复键。
		prefix := ""
		if lower := strings.ToLower(key); strings.HasPrefix(lower, "export ") {
			prefix = key[:len("export ")]
			key = strings.TrimSpace(key[len("export "):])
		}
		if newVal, found := updatesFold[strings.ToLower(key)]; found {
			lines[i] = prefix + key + "=" + quoteEnvValue(newVal)
			applied[strings.ToLower(key)] = true
		}
	}

	// 不存在的键：追加到末尾（前插一个空行分隔）
	var appended []string
	for key, val := range updates {
		if !applied[strings.ToLower(key)] {
			appended = append(appended, key+"="+quoteEnvValue(val))
		}
	}
	if len(appended) > 0 {
		if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
			lines = append(lines, "")
		}
		lines = append(lines, appended...)
	}

	if err := writeLines(path, lines, eol); err != nil {
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
		line := scanner.Text()
		if len(lines) == 0 {
			// 去掉 UTF-8 BOM：否则首行键名会带 \ufeff，大小写归一化也救不回来
			line = strings.TrimPrefix(line, "\ufeff")
		}
		lines = append(lines, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}

// writeLines 原子写入全部行（先写临时文件再重命名，避免中途崩溃损坏配置）。
// eol 指定行尾分隔符（保留原文件风格，默认 "\n"）。
func writeLines(path string, lines []string, eol string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	data := strings.Join(lines, eol) + eol
	if err := os.WriteFile(tmp, []byte(data), 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

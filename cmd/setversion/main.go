// setversion 以 CHANGELOG.md 的最新已发布版本号为唯一来源，
// 自动同步到另外两个位置，保证三处版本号一致：
//   - internal/config/config.go 的 APP_VERSION 默认值（首页 / /health 兜底显示）
//   - .env.example 的 APP_VERSION 行（部署模板）
//
// 用法（项目根目录，发布时改完 CHANGELOG 后执行一次）：
//
//	go run ./cmd/setversion
//
// CHANGELOG.md 中第一条形如 "## [x.y.z] - 日期" 的条目即版本来源
// （[未发布] 段不含数字版本，不会被识别）。
package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
)

var (
	// 匹配 CHANGELOG 中第一条已发布版本条目
	changelogVer = regexp.MustCompile(`(?m)^## \[([0-9]+\.[0-9]+\.[0-9]+)\]`)
	// config.go 默认值中的版本号
	configVer = regexp.MustCompile(`"APP_VERSION": "[0-9]+\.[0-9]+\.[0-9]+"`)
	// .env.example 中的版本号行（兼容 CRLF 与行尾注释前空格）
	envVer = regexp.MustCompile(`(?m)^APP_VERSION=[0-9]+\.[0-9]+\.[0-9]+[ \t]*\r?$`)
)

func main() {
	root, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}

	// 1. 从 CHANGELOG.md 提取版本来源
	cl, err := os.ReadFile(filepath.Join(root, "CHANGELOG.md"))
	if err != nil {
		log.Fatalf("read CHANGELOG.md: %v", err)
	}
	m := changelogVer.FindSubmatch(cl)
	if m == nil {
		log.Fatal("CHANGELOG.md 中未找到已发布版本条目（形如 ## [x.y.z]），请先发布")
	}
	ver := string(m[1])

	// 2. 同步 config.go 默认值
	cfgPath := filepath.Join(root, "internal", "config", "config.go")
	cfg, err := os.ReadFile(cfgPath)
	if err != nil {
		log.Fatalf("read %s: %v", cfgPath, err)
	}
	if !configVer.Match(cfg) {
		log.Fatalf("%s 中未找到 APP_VERSION 默认值", cfgPath)
	}
	cfg = configVer.ReplaceAll(cfg, []byte(fmt.Sprintf(`"APP_VERSION": "%s"`, ver)))
	if err := os.WriteFile(cfgPath, cfg, 0644); err != nil {
		log.Fatalf("write %s: %v", cfgPath, err)
	}

	// 3. 同步 .env.example
	envPath := filepath.Join(root, ".env.example")
	env, err := os.ReadFile(envPath)
	if err != nil {
		log.Fatalf("read %s: %v", envPath, err)
	}
	if !envVer.Match(env) {
		log.Fatalf("%s 中未找到 APP_VERSION 行", envPath)
	}
	env = envVer.ReplaceAll(env, []byte("APP_VERSION="+ver))
	if err := os.WriteFile(envPath, env, 0644); err != nil {
		log.Fatalf("write %s: %v", envPath, err)
	}

	fmt.Printf("✅ 版本已同步为 %s（来源：CHANGELOG.md）\n  - internal/config/config.go\n  - .env.example\n", ver)
}

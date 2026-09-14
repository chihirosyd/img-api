// setversion 以 CHANGELOG.md 的最新已发布版本号为唯一来源，
// 自动同步到代码内置版本号，保证三处版本号一致：
//   - internal/config/config.go 的 APP_VERSION 默认值（首页 / /health 显示）
//   - .env.example 不再含 APP_VERSION（跟随代码内置，环境变量可临时覆盖）
//
// 防呆：
//   - CHANGELOG 版本高于代码版本 → 写入同步
//   - 两者相同 → 空操作（幂等，可重复运行）
//   - CHANGELOG 版本低于代码版本 → 报错退出（提示检查 CHANGELOG 的
//     [未发布] 段是否忘记改名，防止发布旧版本号）
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
	"strconv"
	"strings"
)

var (
	// 匹配 CHANGELOG 中第一条已发布版本条目
	changelogVer = regexp.MustCompile(`(?m)^## \[([0-9]+\.[0-9]+\.[0-9]+)\]`)
	// config.go 默认值中的版本号
	configVer = regexp.MustCompile(`"APP_VERSION": "([0-9]+\.[0-9]+\.[0-9]+)"`)
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

	// 2. 读取 config.go 当前版本
	cfgPath := filepath.Join(root, "internal", "config", "config.go")
	cfg, err := os.ReadFile(cfgPath)
	if err != nil {
		log.Fatalf("read %s: %v", cfgPath, err)
	}
	cur := configVer.FindSubmatch(cfg)
	if cur == nil {
		log.Fatalf("%s 中未找到 APP_VERSION 默认值", cfgPath)
	}
	current := string(cur[1])

	// 3. 防呆比对：新版本必须不低于当前版本（相等 = 幂等空操作）
	cmp := compareSemver(ver, current)
	switch {
	case cmp < 0:
		log.Fatalf("❌ CHANGELOG 版本 %s 低于代码版本 %s：请检查 [未发布] 段是否忘记改名（发布版本号不能倒退）", ver, current)
	case cmp == 0:
		fmt.Printf("✅ 版本已是最新 %s（与 config.go 一致，无需修改）\n", ver)
		return
	}

	// 4. 同步 config.go 默认值
	cfg = configVer.ReplaceAll(cfg, []byte(fmt.Sprintf(`"APP_VERSION": "%s"`, ver)))
	if err := os.WriteFile(cfgPath, cfg, 0644); err != nil {
		log.Fatalf("write %s: %v", cfgPath, err)
	}

	fmt.Printf("✅ 版本已同步 %s → %s（来源：CHANGELOG.md）\n  - internal/config/config.go（.env.example 无需同步，跟随代码内置）\n", current, ver)
}

// compareSemver 比较两个语义版本号，返回 -1/0/1（a 相对 b）。
func compareSemver(a, b string) int {
	pa := parseSemver(a)
	pb := parseSemver(b)
	for i := 0; i < 3; i++ {
		switch {
		case pa[i] < pb[i]:
			return -1
		case pa[i] > pb[i]:
			return 1
		}
	}
	return 0
}

// parseSemver 解析 "x.y.z" 为 [3]int（非法段按 0 处理，保证不 panic）。
func parseSemver(s string) [3]int {
	var out [3]int
	for i, part := range strings.SplitN(s, ".", 3) {
		out[i], _ = strconv.Atoi(part)
	}
	return out
}

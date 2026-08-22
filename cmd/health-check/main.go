// health-check — 向 img-api 发送健康检查请求。
//
// 用法：
//
//	go run ./cmd/health-check/
//	go run ./cmd/health-check/ -url http://localhost:8080
//	go run ./cmd/health-check/ -secret mykey    # 私有模式，完整状态
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

func main() {
	baseURL := flag.String("url", "http://localhost:8080", "img-api server URL")
	secret := flag.String("secret", "", "健康检查密钥（访问完整内部状态）")
	flag.Parse()

	// 去除尾部斜杠，避免 base 为 "http://x/" 时拼出 "//health"
	base := strings.TrimRight(*baseURL, "/")

	// 校验 scheme，避免拼出非 HTTP 地址（如 ftp:// 或纯主机名）
	if u, err := url.Parse(base); err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		fmt.Fprintf(os.Stderr, "❌ invalid -url: must start with http:// or https:// (got %q)\n", base)
		os.Exit(1)
	}

	// 有密钥 → /health-{secret}，否则 → /health
	url := base + "/health"
	if *secret != "" {
		url = base + "/health-" + *secret
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ health check failed: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		fmt.Fprintf(os.Stderr, "❌ decode response: %v\n", err)
		os.Exit(1)
	}

	pretty, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(pretty))

	if resp.StatusCode != http.StatusOK {
		os.Exit(1)
	}
}

// img-api — 高性能随机图片 API 服务。
//
// 零数据库依赖，TXT 文件驱动。支持 PC/手机自适应、多图源切换、
// 外部 API 池按名称和分类筛选、Redis 缓存自动降级、熔断保护。
//
// 启动：go run ./cmd/server/
// 接口：GET /random（详见 docs/API.md）
//
// Windows 桌面用户可改用图形控制面板：go run ./cmd/gui/（发布为 img-api-gui.exe）。
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"img-api/internal/app"
)

func main() {
	// ── 第 1 步：确定项目根目录 ──
	rootPath := app.RootPath()

	// ── 第 2 步：创建服务（加载配置、初始化日志/缓存/路由）──
	srv, err := app.NewServer(rootPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ failed to init server: %v\n", err)
		os.Exit(1)
	}

	// ── 第 3 步：启动监听 ──
	if err := srv.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "❌ failed to start server: %v\n", err)
		os.Exit(1)
	}

	// ── Windows 桌面体验：双击运行的普通用户不了解命令行，
	// 控制台打印友好横幅，并自动打开默认浏览器到教程首页（含运行状态仪表盘），
	// 即可直观确认服务是否正常。自动打开可用环境变量 IMG_API_OPEN_BROWSER=0 关闭。
	if runtime.GOOS == "windows" {
		printWindowsBanner(srv.Port())
		if os.Getenv("IMG_API_OPEN_BROWSER") != "0" {
			app.OpenHome(srv.Port())
		}
	}

	// ── 第 4 步：等待退出信号并优雅关闭 ──
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}

// printWindowsBanner 在 Windows 控制台窗口打印友好启动横幅。
// 日志输出是 JSON 格式，普通用户难以阅读，横幅用人类友好的文字告知：
// 服务在哪、怎么验证、如何停止。
func printWindowsBanner(port int) {
	fmt.Println()
	fmt.Printf("  ✅ img-api 已启动，监听端口 %d\n", port)
	fmt.Printf("  📖 教程首页与运行状态：http://localhost:%d/\n", port)
	fmt.Printf("  🎲 随机图片接口：http://localhost:%d/random\n", port)
	fmt.Println("  ⚠️  关闭此窗口将停止服务")
	fmt.Println()
}



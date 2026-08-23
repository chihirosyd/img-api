// img-api — 高性能随机图片 API 服务。
//
// 零数据库依赖，TXT 文件驱动。支持 PC/手机自适应、多图源切换、
// 外部 API 池按名称和分类筛选、Redis 缓存自动降级、熔断保护。
//
// 启动：go run ./cmd/server/
// 接口：GET /random（详见 docs/API.md）
package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"img-api/internal/app"
	"img-api/internal/cache"
	"img-api/internal/config"
	"img-api/internal/handler"
	"img-api/internal/logger"
	"img-api/internal/middleware"
	"img-api/internal/model"
	"img-api/internal/repository"
	"img-api/internal/service"
)

func main() {
	// ── 第 1 步：确定项目根目录 ──
	rootPath := app.RootPath()

	// ── 第 2 步：加载配置 ──
	if err := config.Load(rootPath); err != nil {
		fmt.Fprintf(os.Stderr, "❌ failed to load config: %v\n", err)
		os.Exit(1)
	}

	// ── 第 3 步：初始化日志 ──
	if err := logger.Init(config.C.LogLevel, filepath.Join(rootPath, config.C.LogDir), config.C.LogMaxAge, config.C.LogMaxSize); err != nil {
		fmt.Fprintf(os.Stderr, "❌ failed to init logger: %v\n", err)
		os.Exit(1)
	}
	logger.L.Info("starting "+config.C.Name, "version", config.C.Version, "port", config.C.Port)

	// ── 第 4 步：初始化缓存（Redis → 内存降级） ──
	var c cache.Cache
	var redisCloser io.Closer
	if config.C.IsRedisEnabled() {
		redisCache, err := cache.NewRedisCache(config.C.RedisAddr, config.C.RedisPassword, config.C.RedisDB)
		if err != nil {
			logger.L.Warn("redis unavailable, falling back to memory cache", "error", err)
			c = cache.NewMemoryCache()
		} else {
			c = redisCache
			redisCloser = redisCache
		}
	} else {
		c = cache.NewMemoryCache()
		logger.L.Info("redis not configured, using memory cache")
	}
	if redisCloser != nil {
		defer redisCloser.Close()
	}

	// ── 第 5 步：初始化外部 API 池 ──
	var externalPool *repository.ExternalPool
	if pool, err := repository.LoadExternalPool(config.Viper()); err != nil {
		logger.L.Warn("failed to load external API pool", "error", err)
	} else {
		externalPool = pool
	}

	// ── 第 6 步：初始化统计 ──
	stats := service.NewStats()

	// ── 第 7 步：初始化服务 ──
	svc := service.NewRandomService(rootPath, c, externalPool, stats)

	// 预热 local/txt 仓库：启动时即生成/加载本地图片索引（local.json 不存在则首次生成），
	// 同时让 /health 从启动起就覆盖 txt 目录的健康状态（txt 仓库初始化很轻量）。
	_ = svc.GetRepo(model.SourceLocal)
	_ = svc.GetRepo(model.SourceTxt)

	// ── 第 8 步：设置 Gin ──
	if !config.C.Debug {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()

	// Gin 默认信任所有反代来源的 X-Forwarded-For（日志 IP 可被伪造）。
	// 与限流器策略一致：仅信任 TRUSTED_PROXIES 配置的网段；未配置则不信任任何转发头。
	if err := r.SetTrustedProxies(config.C.TrustedProxies); err != nil {
		logger.L.Warn("invalid TRUSTED_PROXIES entries ignored", "error", err)
	}

	// ── 第 9 步：注册中间件 ──
	r.Use(gin.Recovery())
	r.Use(middleware.RequestID())
	r.Use(middleware.AccessLog())
	r.Use(middleware.CORS())
	r.Use(middleware.RateLimiter())
	r.Use(middleware.Referer())

	// ── 第 10 步：注册路由 ──
	apiH := handler.NewAPIHandler(rootPath, svc, stats)
	healthH := handler.NewHealthHandler(svc, stats)

	// 健康检查接口：Docker healthcheck、负载均衡探针等场景匿名访问
	r.GET("/health", healthH.Health)

	// 图片接口（/ 根路径在浏览器访问时返回教程首页，<img> 嵌入场景仍出图）
	r.GET("/random", apiH.Random)
	r.GET("/", apiH.Home)

	// ── 第 11 步：优雅启停 ──
	srv := &http.Server{
		Addr:              config.C.ListenAddr(),
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second, // Slowloris 防护：仅限制请求头读取时限
		ReadTimeout:       10 * time.Second,
		// mode=image 代理上限 50MB，慢客户端 30s 写不完会被强制断连，放宽到 120s
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		logger.L.Info("server listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.L.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.L.Info("shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.L.Error("server forced to shutdown", "error", err)
	}

	logger.L.Info("server exited")
}



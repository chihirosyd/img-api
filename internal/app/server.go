package app

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"img-api/internal/cache"
	"img-api/internal/config"
	"img-api/internal/handler"
	"img-api/internal/logger"
	"img-api/internal/middleware"
	"img-api/internal/model"
	"img-api/internal/repository"
	"img-api/internal/service"
)

// Server 封装完整配置好的 HTTP 服务，供两个入口共用：
//   - cmd/server（命令行纯服务）
//   - cmd/gui（Windows 桌面控制面板，进程内启动/停止服务）
//
// NewServer 只做初始化不监听，调用方通过 Start/Shutdown 控制生命周期。
type Server struct {
	rootPath string
	httpSrv  *http.Server
	svc      *service.RandomService // 业务服务（Shutdown 时停止仓库后台任务）

	cacheCloser io.Closer  // 缓存连接（停止时关闭；Redis 客户端或内存缓存的清理协程）
	limiterStop func()     // 限流器后台清理协程停止函数（nil=未启用）

	running atomic.Bool
}

// loggerOnce 保证日志系统只初始化一次：
// GUI 保存设置后重启服务会重复调用 NewServer，重复 Init 会不断新建
// 文件写入器与后台清理协程（goroutine 与文件句柄泄漏）。
var loggerOnce sync.Once

// NewServer 加载配置、初始化日志/缓存/路由，返回待启动的服务实例。
// rootPath 为项目根目录（配置与图库所在地）。
func NewServer(rootPath string) (*Server, error) {
	// ── 加载配置 ──
	if err := config.Load(rootPath); err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	// ── 初始化日志（进程内仅一次）──
	var logErr error
	loggerOnce.Do(func() {
		logErr = logger.Init(config.C.LogLevel, filepath.Join(rootPath, config.C.LogDir),
			config.C.LogMaxAge, config.C.LogMaxSize)
	})
	if logErr != nil {
		return nil, fmt.Errorf("init logger: %w", logErr)
	}
	logger.L.Info("starting "+config.C.Name, "version", config.C.Version, "port", config.C.Port)

	// ── 初始化缓存（Redis → 内存降级）──
	var c cache.Cache
	var cacheCloser io.Closer
	if config.C.IsRedisEnabled() {
		redisCache, err := cache.NewRedisCache(config.C.RedisAddr, config.C.RedisPassword, config.C.RedisDB)
		if err != nil {
			logger.L.Warn("redis unavailable, falling back to memory cache", "error", err)
			mc := cache.NewMemoryCache()
			c = mc
			cacheCloser = mc
		} else {
			c = redisCache
			cacheCloser = redisCache
		}
	} else {
		mc := cache.NewMemoryCache()
		c = mc
		cacheCloser = mc
		logger.L.Info("redis not configured, using memory cache")
	}

	// ── 初始化外部 API 池 ──
	var externalPool *repository.ExternalPool
	if pool, err := repository.LoadExternalPool(config.Image); err != nil {
		logger.L.Warn("failed to load external API pool", "error", err)
	} else {
		externalPool = pool
	}

	// ── 初始化统计与服务 ──
	stats := service.NewStats()
	svc := service.NewRandomService(rootPath, c, externalPool, stats)

	// 预热 local/txt 仓库：启动时即生成/加载本地图片索引（local.json 不存在则首次生成），
	// 同时让 /health 从启动起就覆盖 txt 目录的健康状态（txt 仓库初始化很轻量）。
	_ = svc.GetRepo(model.SourceLocal)
	_ = svc.GetRepo(model.SourceTxt)

	// ── 设置路由与中间件（net/http 标准库）──
	apiH := handler.NewAPIHandler(rootPath, svc, stats)
	healthH := handler.NewHealthHandler(svc, stats)

	mux := http.NewServeMux()
	// 健康检查接口：Docker healthcheck、负载均衡探针等场景匿名访问
	mux.HandleFunc("GET /health", healthH.Health)
	// 图片接口（/ 根路径在浏览器访问时返回教程首页，<img> 嵌入场景仍出图）
	mux.HandleFunc("GET /random", apiH.Random)
	mux.HandleFunc("GET /", apiH.Home)

	// 中间件包装顺序（后包装的在最外层，即先执行）：
	// Recovery → RequestID → AccessLog → RateLimiter → CORS → Referer → mux
	// （RateLimiter 在 CORS 外层：OPTIONS 预检同样计入限流，防无限制预检洪泛）
	var h http.Handler = mux
	h = middleware.Referer()(h)
	h = middleware.CORS()(h)
	rateLimitMW, limiterStop := middleware.RateLimiter()
	h = rateLimitMW(h)
	h = middleware.AccessLog()(h)
	h = middleware.RequestID()(h)
	h = middleware.Recovery()(h)

	return &Server{
		rootPath:    rootPath,
		svc:         svc,
		cacheCloser: cacheCloser,
		limiterStop: limiterStop,
		httpSrv: &http.Server{
			Addr:              config.C.ListenAddr(),
			Handler:           h,
			ReadHeaderTimeout: 5 * time.Second, // Slowloris 防护：仅限制请求头读取时限
			ReadTimeout:       10 * time.Second,
			// mode=image 代理上限 50MB，慢客户端 30s 写不完会被强制断连，放宽到 120s
			WriteTimeout: 120 * time.Second,
			IdleTimeout:  60 * time.Second,
		},
	}, nil
}

// Start 启动监听（非阻塞）。已运行时重复调用会被忽略。
func (s *Server) Start() error {
	if s.running.Load() {
		return nil
	}

	// 先绑定端口再进入 Serve 循环：端口占用等错误同步返回，
	// 消除此前"50ms 探测窗口"吞掉延迟 listen 错误的问题。
	ln, err := net.Listen("tcp", s.httpSrv.Addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", s.httpSrv.Addr, err)
	}

	go func() {
		logger.L.Info("server listening", "addr", s.httpSrv.Addr)
		if err := s.httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
			logger.L.Error("server error", "error", err)
		}
	}()

	s.running.Store(true)
	return nil
}

// Shutdown 优雅停止服务（可重复调用）。
func (s *Server) Shutdown(ctx context.Context) error {
	if !s.running.Swap(false) {
		return nil
	}
	logger.L.Info("shutting down server...")
	// 先停后台任务（限流清理协程、仓库 cron 调度器），再停 HTTP 与缓存
	if s.limiterStop != nil {
		s.limiterStop()
	}
	if s.svc != nil {
		s.svc.Close()
	}
	if err := s.httpSrv.Shutdown(ctx); err != nil {
		logger.L.Error("server forced to shutdown", "error", err)
		return err
	}
	if s.cacheCloser != nil {
		_ = s.cacheCloser.Close()
	}
	logger.L.Info("server exited")
	return nil
}

// Running 返回服务是否正在监听。
func (s *Server) Running() bool { return s.running.Load() }

// Port 返回当前配置的监听端口。
func (s *Server) Port() int { return config.C.Port }

// HomeURL 返回教程首页地址（含运行状态仪表盘）。
func (s *Server) HomeURL() string { return fmt.Sprintf("http://localhost:%d/", config.C.Port) }

// OpenHome 打开默认浏览器访问教程首页。静默失败：打不开浏览器不影响服务运行。
func OpenHome(port int) {
	OpenURL(fmt.Sprintf("http://localhost:%d/", port))
}

// OpenURL 打开默认浏览器访问指定地址（跨平台；失败仅记 Debug 日志）。
func OpenURL(rawURL string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL)
	case "darwin":
		cmd = exec.Command("open", rawURL)
	default:
		cmd = exec.Command("xdg-open", rawURL)
	}
	if err := cmd.Start(); err != nil {
		logger.L.Debug("open url failed", "url", rawURL, "error", err)
	}
}

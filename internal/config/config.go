// Package config 负责加载和管理应用配置。
//
// 使用 Viper 支持多种配置格式（.env / YAML / JSON），
// 提供类型安全的全局配置访问。
//
// 使用方式：
//
//	config.Load(rootPath)     // 启动时调用一次
//	config.C.Debug            // 访问配置项
//	config.Viper()            // 获取原始 Viper 实例
package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/viper"
)

// C 是全局配置单例，Load() 调用后即可安全使用。
var C *AppConfig

// v 是内部 Viper 实例（供 ExternalPool 等模块读取 YAML 配置）。
var v *viper.Viper

// Viper 返回底层 Viper 实例。
// 仅当需要直接读取 Viper 配置（如 Unmarshal YAML）时使用。
func Viper() *viper.Viper { return v }

// AppConfig 聚合所有应用配置项，与 .env.example 一一对应。
type AppConfig struct {
	Debug bool // true: Gin debug 模式 + 日志显示调用位置

	Name string // 应用名称（用于日志标识）
	Host string // HTTP 监听地址（0.0.0.0 表示监听所有网卡）
	Port int    // HTTP 监听端口
	Version string // 语义版本号（影响 /health 返回和启动日志）

	AuthToken   string // 鉴权密钥（客户端需携带相同 token）
	AuthEnabled bool   // 是否启用 Token 鉴权
	CorsEnabled      bool // 是否启用 CORS 头（Nginx 反代时可关闭）
	RateLimitEnabled bool // 是否启用 IP 频率限制
	RateLimitMax     int  // 每分钟每 IP 最大请求数

	RefererWhitelist []string // 防盗链域名白名单（空 = 不限制）
	TrustedProxies   []string // 可信反代网段（CIDR），限流时仅信任这些来源的转发头

	DefaultSource string // 默认图片源类型：txt / local / external

	LocalIndexRefreshMinutes int // local 索引自动刷新间隔（分钟，0=仅启动时生成一次）

	RedisAddr     string // Redis 地址（ip:port），空字符串表示不启用
	RedisPassword string // Redis 密码
	RedisDB       int    // Redis 数据库编号（0~15）

	CircuitFailureThreshold int // 熔断器连续失败多少次后触发断路
	CircuitTimeoutSeconds   int // 断路后多少秒尝试半开探测
	CircuitHalfOpenMax      int // 半开状态最多放行多少个探测请求

	HealthSecret string // 健康检查密钥（非空时 /health 仅返回极简状态）

	LogLevel   string // 日志级别：debug / info / warn / error
	LogDir     string // 日志文件存放目录（相对于项目根路径）
	LogMaxAge  int    // 日志保留天数（0 = 永久保留）
	LogMaxSize int    // 单文件最大 MB（0 = 不限制）
}

// Load 从指定项目根目录加载所有配置。
//
// 配置优先级（高 → 低）：系统环境变量 > .env 文件 > 默认值。
// configs/image.yaml 单独供外部 API 池模块（ExternalPool）读取，
// 不在环境变量/默认值体系内。
//
// 任意配置文件缺失都不报错，确保最小可用；
// 文件存在但解析失败时向 stderr 输出警告（此时日志系统尚未初始化）。
func Load(rootPath string) error {
	v = viper.New()

	// 步骤 1 — .env 文件（可选）
	v.SetConfigFile(rootPath + "/.env")
	v.SetConfigType("env")
	if err := v.ReadInConfig(); err != nil {
		// 文件缺失属正常（使用默认值）；存在但解析失败时提示用户
		// （此时日志系统尚未初始化，输出到 stderr）
		var nf viper.ConfigFileNotFoundError
		if !errors.As(err, &nf) {
			fmt.Fprintf(os.Stderr, "⚠️  failed to parse .env: %v\n", err)
		}
	}

	// 步骤 2 — 环境变量覆盖（APP_DEBUG → app_debug）
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// 步骤 3 — 外部 API 池 YAML 配置（可选）
	v.SetConfigFile(rootPath + "/configs/image.yaml")
	v.SetConfigType("yaml")
	if err := v.MergeInConfig(); err != nil {
		// image.yaml 不是必须的，缺失不报错；存在但解析失败时提示
		var nf viper.ConfigFileNotFoundError
		if !errors.As(err, &nf) {
			fmt.Fprintf(os.Stderr, "⚠️  failed to parse configs/image.yaml: %v\n", err)
		}
	}

	// 步骤 4 — 写入所有默认值
	setDefaults(v)

	// 步骤 5 — 映射到类型安全的结构体
	C = &AppConfig{
		Debug: v.GetBool("app_debug"),
		Name:  v.GetString("app_name"),
		Host:  v.GetString("app_host"),
		Port:  v.GetInt("app_port"),
		Version: v.GetString("app_version"),

		AuthToken:   v.GetString("auth_token"),
		AuthEnabled: v.GetBool("auth_enabled"),

		CorsEnabled:      v.GetBool("cors_enabled"),
		RateLimitEnabled: v.GetBool("rate_limit_enabled"),
		RateLimitMax:     v.GetInt("rate_limit_max"),

		RefererWhitelist: parseWhitelist(v.GetString("referer_whitelist")),
		TrustedProxies:   parseWhitelist(v.GetString("trusted_proxies")),

		DefaultSource: v.GetString("default_source"),

		LocalIndexRefreshMinutes: v.GetInt("local_index_refresh_minutes"),

		RedisAddr:     v.GetString("redis_addr"),
		RedisPassword: v.GetString("redis_password"),
		RedisDB:       v.GetInt("redis_db"),

		CircuitFailureThreshold: v.GetInt("circuit_failure_threshold"),
		CircuitTimeoutSeconds:   v.GetInt("circuit_timeout_seconds"),
		CircuitHalfOpenMax:      v.GetInt("circuit_half_open_max"),

		HealthSecret: v.GetString("health_secret"),

		LogLevel:   v.GetString("log_level"),
		LogDir:     v.GetString("log_dir"),
		LogMaxAge:  v.GetInt("log_max_age"),
		LogMaxSize: v.GetInt("log_max_size"),
	}

	// 边界值校验：非法配置（<=0）回退到默认值，避免限流器把全部请求拒之门外。
	if C.RateLimitMax <= 0 {
		C.RateLimitMax = 60
	}
	if C.CircuitFailureThreshold <= 0 {
		C.CircuitFailureThreshold = 5
	}
	if C.CircuitTimeoutSeconds <= 0 {
		C.CircuitTimeoutSeconds = 30
	}
	if C.CircuitHalfOpenMax <= 0 {
		C.CircuitHalfOpenMax = 1
	}

	// ── 配置健全性告警（日志系统尚未初始化，输出到 stderr）──
	if C.AuthEnabled && C.AuthToken == "" {
		fmt.Fprintf(os.Stderr, "⚠️  AUTH_ENABLED=true 但 AUTH_TOKEN 为空：所有图片请求都将被 401 拒绝\n")
	}
	for name, val := range map[string]string{
		"auth_token": C.AuthToken, "redis_password": C.RedisPassword, "health_secret": C.HealthSecret,
	} {
		if strings.Contains(val, "$") {
			fmt.Fprintf(os.Stderr, "⚠️  配置 %s 包含 $：.env 解析器会将其作为变量展开，请用单引号包裹该值（详见 docs/CONFIG.md）\n", name)
		}
	}

	return nil
}

// parseWhitelist 将逗号分隔的字符串解析为切片。
//
//	"a.com, b.com" → ["a.com", "b.com"]
//	""              → nil
//	" , , "         → nil（全部为空项时返回 nil，而非空切片：
//	                   调用方以 nil 作为"未配置"的哨兵值）
func parseWhitelist(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// setDefaults 注册所有配置项的默认值。
// 在 .env 和环境变量之后调用，确保只对未设置的项生效。
func setDefaults(v *viper.Viper) {
	v.SetDefault("app_debug", false) // 默认关闭调试，避免公网部署暴露分类清单/详细错误
	v.SetDefault("app_name", "img-api")
	v.SetDefault("app_host", "0.0.0.0")
	v.SetDefault("app_version", "1.0.0")
	v.SetDefault("app_port", 8080)

	v.SetDefault("auth_enabled", false)
	v.SetDefault("auth_token", "")

	v.SetDefault("cors_enabled", true)
	v.SetDefault("rate_limit_enabled", true)
	v.SetDefault("rate_limit_max", 60)

	// 防盗链默认关闭（与 .env.example 的 REFERER_WHITELIST= 一致）。
	// 启用时填写白名单域名，如 "mysite.com,blog.mysite.com"
	v.SetDefault("referer_whitelist", "")

	// 可信反代网段：仅当请求来自这些网段时限流器才信任 X-Forwarded-For。
	// 留空 = 不信任任何转发头（防伪造），直连部署无需配置。
	v.SetDefault("trusted_proxies", "")

	v.SetDefault("default_source", "txt")
	v.SetDefault("local_index_refresh_minutes", 0) // 0=不自动刷新，仅启动时生成一次

	v.SetDefault("redis_addr", "")
	v.SetDefault("redis_password", "")
	v.SetDefault("redis_db", 0)

	v.SetDefault("circuit_failure_threshold", 5)
	v.SetDefault("circuit_timeout_seconds", 30)
	v.SetDefault("circuit_half_open_max", 3)

	v.SetDefault("health_secret", "")

	v.SetDefault("log_max_age", 30)
	v.SetDefault("log_max_size", 0)
	v.SetDefault("log_level", "info")
	v.SetDefault("log_dir", "storage/logs")
}

// IsRedisEnabled 判断 Redis 是否已配置（地址非空）。
func (c *AppConfig) IsRedisEnabled() bool {
	return c.RedisAddr != ""
}

// ListenAddr 返回 "host:port" 格式的监听地址。
func (c *AppConfig) ListenAddr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

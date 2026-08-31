// Package config 负责加载和管理应用配置。
//
// 使用轻量实现（godotenv + 环境变量 + yaml.v3），不依赖 Viper：
//   - .env 文件（godotenv，支持引号/$ 展开/export 前缀）
//   - 环境变量覆盖（系统环境变量 > .env > 默认值）
//   - config/image.yaml 独立解析（见 imagecfg.go）
//
// 使用方式：
//
//	config.Load(rootPath)     // 启动时调用一次
//	config.C.Debug            // 访问配置项
//	config.Image              // 访问 image.yaml（外部 API 池）
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"
)

// C 是全局配置单例，Load() 调用后即可安全使用。
var C *AppConfig

// AppConfig 聚合所有应用配置项，与 .env.example 一一对应。
type AppConfig struct {
	Debug bool // true: 调试模式（错误附详细信息、提示页附可用列表、日志附源码位置）

	Name string // 应用名称（用于日志标识）
	Host string // HTTP 监听地址（0.0.0.0 表示监听所有网卡）
	Port int    // HTTP 监听端口
	Version string // 语义版本号（影响 /health 返回和启动日志）

	CorsEnabled      bool // 是否启用跨域响应头（<img> 嵌图无需跨域；Nginx 处理时可关闭）
	RateLimitEnabled bool // 是否启用内置 IP 限流（纯内存实现）
	RateLimitMax     int  // 每分钟每 IP 最大请求数

	RefererWhitelist []string // 防盗链域名白名单（空 = 不限制；反代转发后依然生效）
	TrustedProxies   []string // 可信反代网段（CIDR），限流与访问日志仅信任这些来源的转发头

	DefaultSource string // 默认图片源类型：txt / local / external

	// 渠道默认分类：请求不带 category（或显式传 "default"）时使用。
	// 留空 = 内置 "default"（default.txt / default 目录）
	TxtDefaultCategory   string
	LocalDefaultCategory string

	// local 索引自动刷新计划：0/空=仅启动时生成一次；
	// Go duration（30s/30m/24h/168h）；@ 描述符（@daily/@weekly/@monthly/@yearly）；5 字段 cron
	LocalIndexRefresh string

	RedisAddr     string // Redis 地址（ip:port），空字符串表示不启用
	RedisPassword string // Redis 密码
	RedisDB       int    // Redis 数据库编号（0~15）

	CircuitFailureThreshold int // 熔断器连续失败多少次后触发断路
	CircuitTimeoutSeconds   int // 断路后多少秒尝试半开探测
	CircuitHalfOpenMax      int // 半开状态最多放行多少个探测请求

	LogLevel   string // 日志级别：debug / info / warn / error
	LogDir     string // 日志文件存放目录（相对于项目根路径）
	LogMaxAge  int    // 日志保留天数（0 = 永久保留）
	LogMaxSize int    // 单文件最大 MB（0 = 不限制）
}

// Load 从指定项目根目录加载所有配置。
//
// 配置优先级（高 → 低）：系统环境变量 > .env 文件 > 默认值。
// 键名大小写不敏感（解析时统一归一化，推荐环境变量与 .env 全大写）。
// config/image.yaml 单独供外部 API 池模块（ExternalPool）读取，
// 不在环境变量/默认值体系内。
//
// 任意配置文件缺失都不报错，以保持最小可用；
// 文件存在但解析失败时向 stderr 输出警告（此时日志系统尚未初始化）。
func Load(rootPath string) error {
	// 步骤 1 — .env 文件（可选，godotenv 完整支持引号、$ 展开、export 前缀）
	values := map[string]string{}
	envFile := rootPath + "/.env"
	if _, statErr := os.Stat(envFile); statErr == nil {
		m, err := godotenv.Read(envFile)
		if err != nil {
			// 文件存在但解析失败：此时日志系统尚未初始化，输出到 stderr
			fmt.Fprintf(os.Stderr, "⚠️  failed to parse .env: %v\n", err)
		} else {
			// 键名归一化为大写：.env 中 app_port 与 APP_PORT 等价（大小写不敏感）
			values = make(map[string]string, len(m))
			for k, val := range m {
				values[strings.ToUpper(k)] = val
			}
		}
	}

	// 步骤 2 — 取值器：环境变量 > .env > 默认值（键名大小写不敏感）
	getStr := func(key string) string {
		if v, ok := lookupEnvFold(key); ok {
			return v
		}
		if v, ok := values[strings.ToUpper(key)]; ok {
			return v
		}
		return defaults[key]
	}
	getBool := func(key string) bool {
		b, err := strconv.ParseBool(getStr(key))
		return err == nil && b
	}
	getInt := func(key string) int {
		n, err := strconv.Atoi(strings.TrimSpace(getStr(key)))
		if err != nil {
			if d, ok := defaults[key]; ok {
				if dn, err2 := strconv.Atoi(d); err2 == nil {
					return dn
				}
			}
			return 0
		}
		return n
	}

	// 步骤 3 — 外部 API 池 YAML 配置（可选）
	Image = &ImageConfig{}
	imageFile := rootPath + "/config/image.yaml"
	if _, statErr := os.Stat(imageFile); statErr == nil {
		b, err := os.ReadFile(imageFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "⚠️  failed to read config/image.yaml: %v\n", err)
		} else if err := yaml.Unmarshal(b, Image); err != nil {
			fmt.Fprintf(os.Stderr, "⚠️  failed to parse config/image.yaml: %v\n", err)
		}
	}

	// 步骤 4 — 映射到类型安全的结构体
	C = &AppConfig{
		Debug:   getBool("APP_DEBUG"),
		Name:    getStr("APP_NAME"),
		Host:    getStr("APP_HOST"),
		Port:    getInt("APP_PORT"),
		Version: getStr("APP_VERSION"),

		CorsEnabled:      getBool("CORS_ENABLED"),
		RateLimitEnabled: getBool("RATE_LIMIT_ENABLED"),
		RateLimitMax:     getInt("RATE_LIMIT_MAX"),

		RefererWhitelist: parseWhitelist(getStr("REFERER_WHITELIST")),
		TrustedProxies:   parseWhitelist(getStr("TRUSTED_PROXIES")),

		DefaultSource: getStr("DEFAULT_SOURCE"),

		TxtDefaultCategory:   strings.TrimSpace(getStr("TXT_DEFAULT_CATEGORY")),
		LocalDefaultCategory: strings.TrimSpace(getStr("LOCAL_DEFAULT_CATEGORY")),

		LocalIndexRefresh: strings.TrimSpace(getStr("LOCAL_INDEX_REFRESH")),

		RedisAddr:     getStr("REDIS_ADDR"),
		RedisPassword: getStr("REDIS_PASSWORD"),
		RedisDB:       getInt("REDIS_DB"),

		CircuitFailureThreshold: getInt("CIRCUIT_FAILURE_THRESHOLD"),
		CircuitTimeoutSeconds:   getInt("CIRCUIT_TIMEOUT_SECONDS"),
		CircuitHalfOpenMax:      getInt("CIRCUIT_HALF_OPEN_MAX"),

		LogLevel:   getStr("LOG_LEVEL"),
		LogDir:     getStr("LOG_DIR"),
		LogMaxAge:  getInt("LOG_MAX_AGE"),
		LogMaxSize: getInt("LOG_MAX_SIZE"),
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
	for name, val := range map[string]string{
		"redis_password": C.RedisPassword,
		"app_name":       C.Name, "app_host": C.Host, "default_source": C.DefaultSource,
		"redis_addr": C.RedisAddr,
		"referer_whitelist": strings.Join(C.RefererWhitelist, ","),
		"trusted_proxies":   strings.Join(C.TrustedProxies, ","),
	} {
		if strings.Contains(val, "$") {
			fmt.Fprintf(os.Stderr, "⚠️  配置 %s 包含 $：.env 解析器会将其作为变量展开，请用单引号包裹该值（详见 docs/CONFIG.md）\n", name)
		}
	}

	return nil
}

// lookupEnvFold 大小写不敏感地查找环境变量（统一跨平台行为，
// 避免 Linux 上 os.LookupEnv 区分大小写导致小写键不生效）。
func lookupEnvFold(key string) (string, bool) {
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok && strings.EqualFold(k, key) {
			return v, true
		}
	}
	return "", false
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

// defaults 注册所有配置项的默认值。
// .env 和环境变量都未设置时生效。
var defaults = map[string]string{
	// 默认关闭调试，避免公网部署暴露分类清单/详细错误
	"APP_DEBUG": "false",
	"APP_NAME":  "img-api",
	"APP_HOST":  "0.0.0.0",
	"APP_VERSION": "1.4.8",
	"APP_PORT":   "8080",

	"CORS_ENABLED":       "true",
	"RATE_LIMIT_ENABLED": "true",
	"RATE_LIMIT_MAX":     "60",

	// 防盗链默认关闭（与 .env.example 的 REFERER_WHITELIST= 一致）。
	// 启用时填写白名单域名，如 "mysite.com,blog.mysite.com"
	"REFERER_WHITELIST": "",

	// 可信反代网段：仅当请求来自这些网段时限流器才信任 X-Forwarded-For。
	// 留空 = 不信任任何转发头（防伪造），直连部署无需配置。
	"TRUSTED_PROXIES": "",

	"DEFAULT_SOURCE": "txt",

	// 刷新计划：0/空=仅启动时生成一次；详见 LOCAL_INDEX_REFRESH 注释
	"LOCAL_INDEX_REFRESH": "",

	// 默认分类：留空 = 内置 "default"（txt 的 default.txt / local 的 default 目录）
	"TXT_DEFAULT_CATEGORY":   "",
	"LOCAL_DEFAULT_CATEGORY": "",

	"REDIS_ADDR":     "",
	"REDIS_PASSWORD": "",
	"REDIS_DB":       "0",

	"CIRCUIT_FAILURE_THRESHOLD": "5",
	"CIRCUIT_TIMEOUT_SECONDS":   "30",
	"CIRCUIT_HALF_OPEN_MAX":     "3",

	"LOG_MAX_AGE":  "30",
	"LOG_MAX_SIZE": "0",
	"LOG_LEVEL":    "info",
	"LOG_DIR":      "storage/logs",
}

// IsRedisEnabled 判断 Redis 是否已配置（地址非空）。
func (c *AppConfig) IsRedisEnabled() bool {
	return c.RedisAddr != ""
}

// ListenAddr 返回 "host:port" 格式的监听地址。
func (c *AppConfig) ListenAddr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

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
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"
)

// C 是全局配置单例，Load() 调用后即可安全使用。
var C *AppConfig

// ImageWarnings 收集 image.yaml 的读取/解析失败信息。
//
// config.Load 在日志系统初始化之前执行，问题只能先写 stderr；调用方
// （app.NewServer）应在 logger 就绪后遍历本切片以 Error 级再记一次，
// 否则 GUI 用户看不到任何提示（GUI 无控制台，stderr 形同丢弃），
// 表现为"配了 image.yaml 却不生效"且毫无线索。
var ImageWarnings []string

// Warnings 收集 .env / 环境变量解析期间的非致命问题（解析失败、非法数值、
// 非法布尔值、空值覆盖等）。与 ImageWarnings 同理：Load 阶段日志系统还没就绪，
// 只能写 stderr；GUI 没有控制台，必须由调用方在日志就绪后补记到日志文件。
var Warnings []string

// warnf 记录一条配置告警：写入 Warnings 并同时输出到 stderr。
func warnf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	Warnings = append(Warnings, msg)
	fmt.Fprintln(os.Stderr, "⚠️  "+msg)
}

// AppConfig 聚合所有应用配置项，与 .env.example 一一对应。
type AppConfig struct {
	Debug bool // true: 调试模式（错误附详细信息、提示页附可用列表、日志附源码位置）

	Name    string // 应用名称（用于日志标识）
	Host    string // HTTP 监听地址（0.0.0.0 表示监听所有网卡）
	Port    int    // HTTP 监听端口
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
			// 文件存在但解析失败：此时日志系统尚未初始化，输出到 stderr。
			// 必须说清后果——godotenv 失败时整个文件都不会生效（常见触发是值里的
			// `$` 展开失败），此前只报 "failed to parse"，用户会以为"改的配置已生效"。
			fmt.Fprintf(os.Stderr,
				"⚠️  failed to parse .env: %v\n"+
					"    注意：解析失败时**整个 .env 都会被忽略**，所有配置回落默认值/系统环境变量。\n"+
					"    常见原因：值里的 $ 被当作变量展开（请用单引号包裹，如 REDIS_PASSWORD='pa$sw0rd'）。\n", err)
			Warnings = append(Warnings, fmt.Sprintf("parse .env: %v（整份 .env 已忽略）", err))
		} else {
			// 键名归一化为大写：.env 中 app_port 与 APP_PORT 等价（大小写不敏感）
			values = make(map[string]string, len(m))
			for k, val := range m {
				values[strings.ToUpper(k)] = val
			}
		}
	}

	// 步骤 2 — 取值器：环境变量 > .env > 默认值（键名大小写不敏感）
	//
	// 空值视为"未设置"：Docker Compose 里写 `APP_HOST:` 会注入空字符串，
	// 若当成有效值，`APP_HOST=` 会让服务监听 0.0.0.0（绕过 .env 的 127.0.0.1），
	// `LOG_DIR=` 则会因 MkdirAll("") 直接启动失败。
	getStr := func(key string) string {
		if v, ok := lookupEnvFold(key); ok && strings.TrimSpace(v) != "" {
			return v
		}
		if v, ok := values[strings.ToUpper(key)]; ok && strings.TrimSpace(v) != "" {
			return v
		}
		return defaults[key]
	}
	getBool := func(key string) bool {
		raw := strings.TrimSpace(getStr(key))
		b, err := strconv.ParseBool(raw)
		if err != nil {
			// 与 getInt 同思路：非法值回退 defaults，而不是恒返回 false，
			// 否则 RATE_LIMIT_ENABLED=yes 会静默把限流关掉（默认是 true）。
			d, ok := defaults[key]
			db, derr := strconv.ParseBool(d)
			if ok && derr == nil {
				if raw != "" {
					warnf("%s=%q 不是合法布尔值（true/false），已回退默认值 %t", key, raw, db)
				}
				return db
			}
			if raw != "" {
				warnf("%s=%q 不是合法布尔值（true/false），已回退 false", key, raw)
			}
			return false
		}
		return b
	}
	getInt := func(key string) int {
		raw := strings.TrimSpace(getStr(key))
		n, err := strconv.Atoi(raw)
		if err != nil {
			d, ok := defaults[key]
			dn, derr := strconv.Atoi(d)
			if ok && derr == nil {
				// 此前 Atoi 失败直接回退默认、无任何提示，注释宣称的告警并未生效
				if raw != "" {
					warnf("%s=%q 不是合法整数，已回退默认 %s", key, raw, d)
				}
				return dn
			}
			if raw != "" {
				warnf("%s=%q 不是合法整数，已回退 0", key, raw)
			}
			return 0
		}
		return n
	}

	// 步骤 3 — 外部 API 池 YAML 配置（可选）
	Image = &ImageConfig{}
	ImageWarnings = nil
	imageFile := rootPath + "/config/image.yaml"
	if _, statErr := os.Stat(imageFile); statErr == nil {
		b, err := os.ReadFile(imageFile)
		if err != nil {
			msg := fmt.Sprintf("read config/image.yaml: %v", err)
			ImageWarnings = append(ImageWarnings, msg)
			fmt.Fprintf(os.Stderr, "⚠️  failed to %s\n", msg)
		} else {
			// KnownFields：拼错键名（如 external_api）或多写未知字段时直接报错，
			// 而不是静默忽略——"配了却不生效"是这类 YAML 最常见的坑。
			dec := yaml.NewDecoder(bytes.NewReader(b))
			dec.KnownFields(true)
			if err := dec.Decode(Image); err != nil {
				// 解析失败时整个文件都不生效（external 池为空 → source=external 返回
				// 503 引导页）。记录到 ImageWarnings，供 logger 就绪后以 Error 级落盘。
				msg := fmt.Sprintf("parse config/image.yaml: %v", err)
				ImageWarnings = append(ImageWarnings, msg)
				fmt.Fprintf(os.Stderr, "⚠️  failed to %s\n"+
					"    注意：解析失败时**整份 image.yaml 均不生效**，source=external 会返回未配置引导页。\n", msg)
			}
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
	// 回退值必须与 defaults 里的 "3" 一致：raw=abc 由 getInt 回退到默认 3，
	// raw<=0 走这里；否则同一种"非法"会出现 3 与 1 两种结果。
	if C.CircuitHalfOpenMax <= 0 {
		C.CircuitHalfOpenMax = 3
	}
	// 端口范围校验：非法值（0 会绑定随机端口、>65535 启动失败）回退默认并明确告警，
	// 避免"静默改端口"（此前 APP_PORT=abc 静默用 8080、=0 静默绑随机端口均无提示）
	if C.Port <= 0 || C.Port > 65535 {
		warnf("APP_PORT 非法（%d），已回退默认 8080（合法范围 1~65535）", C.Port)
		C.Port = 8080
	}
	// 日志级别归一化 + 非法值告警：项目其余枚举值（type/source/mode/DEFAULT_SOURCE）
	// 都做了小写归一化，此前 LOG_LEVEL 却是精确匹配——写 DEBUG / Info 会静默回落到
	// info 且无任何提示，与"配置项大小写不敏感"的声明不一致。
	switch strings.ToLower(strings.TrimSpace(C.LogLevel)) {
	case "debug", "info", "warn", "error":
		C.LogLevel = strings.ToLower(strings.TrimSpace(C.LogLevel))
	default:
		warnf("LOG_LEVEL 非法（%q），已回退默认 info（可选：debug / info / warn / error）", C.LogLevel)
		C.LogLevel = "info"
	}

	// ── 配置健全性告警（日志系统尚未初始化，输出到 stderr）──
	// 覆盖全部字符串配置项（与 CHANGELOG 的承诺一致）
	for _, c := range []struct{ name, value string }{
		{"app_name", C.Name}, {"app_host", C.Host}, {"app_version", C.Version},
		{"default_source", C.DefaultSource},
		{"txt_default_category", C.TxtDefaultCategory},
		{"local_default_category", C.LocalDefaultCategory},
		{"local_index_refresh", C.LocalIndexRefresh},
		{"redis_addr", C.RedisAddr}, {"redis_password", C.RedisPassword},
		{"referer_whitelist", strings.Join(C.RefererWhitelist, ",")},
		{"trusted_proxies", strings.Join(C.TrustedProxies, ",")},
		{"log_level", C.LogLevel}, {"log_dir", C.LogDir},
	} {
		if strings.Contains(c.value, "$") {
			warnf("配置 %s 包含 $：.env 解析器会将其作为变量展开，请用单引号包裹该值（详见 docs/CONFIG.md）", c.name)
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
	"APP_DEBUG":   "false",
	"APP_NAME":    "img-api",
	"APP_HOST":    "0.0.0.0",
	"APP_VERSION": "1.6.0",
	"APP_PORT":    "8080",

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

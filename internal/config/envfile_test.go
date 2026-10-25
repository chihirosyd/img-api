package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReadEnvValuesStripsInlineComment 回归 P5-4：
// GUI 读取 .env 时必须剥离行内注释，否则会把注释当成值显示/回写
// （如端口显示成 "8080  # 端口"），与主服务的解析结果不一致。
func TestReadEnvValuesStripsInlineComment(t *testing.T) {
	dir := t.TempDir()
	content := "# 注释行\n" +
		"APP_PORT=9090        # 监听端口\n" +
		"RATE_LIMIT_ENABLED=false\n" +
		"REDIS_PASSWORD='pa#ss'\n" +
		"APP_DEBUG=true\n"
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	values, err := ReadEnvValues(dir)
	if err != nil {
		t.Fatalf("ReadEnvValues: %v", err)
	}

	cases := map[string]string{
		"APP_PORT":           "9090", // 行内注释被剥离
		"RATE_LIMIT_ENABLED": "false",
		"APP_DEBUG":          "true",
		"REDIS_PASSWORD":     "pa#ss", // 引号被剥掉、内部 # 保留（与 godotenv 一致）
	}
	for key, want := range cases {
		if got := values[key]; got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
}

// TestStripInlineComment 单元覆盖剥离规则本身。
func TestStripInlineComment(t *testing.T) {
	cases := []struct{ in, want string }{
		{"30   # 保留天数", "30"},
		{"0         # 单文件最大 MB", "0"},
		{"plain", "plain"},
		{"'pa#ss'", "pa#ss"},     // 成对单引号被剥掉，# 保留
		{`"qu#oted"`, `qu#oted`}, // 成对双引号同理
		{`"true"  # 开关`, `true`}, // 引号后跟注释
		{"pa#ss", "pa#ss"},       // # 前不是空白：属于值本身，不截断
		{"foo#baz", "foo#baz"},   // 同上，取自 godotenv 夹具 bar=foo#baz
		{"foo #baz", "foo"},      // 空白之后的 # 仍是注释
		{"", ""},
		{"   # 全注释", ""},
	}
	for _, c := range cases {
		if got := stripInlineComment(c.in); got != c.want {
			t.Errorf("stripInlineComment(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestReadEnvValuesInlineCommentMatchesGodotenv 回归 C-01：
// godotenv 只在 # 的前一个字符是空白时才把 # 当行内注释起点。前三个键取自依赖自带的
// fixtures/comments.env，期望值与 godotenv 的 TestComments 完全相同；
// 此前 GUI 见 # 就截断，REDIS_PASSWORD=pa#ss 这类值保存一次就被静默改写成 "pa"。
func TestReadEnvValuesInlineCommentMatchesGodotenv(t *testing.T) {
	dir := t.TempDir()
	content := "# 整行注释\n" +
		"foo=bar # baz\n" +
		"bar=foo#baz\n" +
		`baz="foo"#bar` + "\n" +
		"COMMENTED=foo #baz\n" +
		"REDIS_PASSWORD=pa#ss\n" +
		`QUOTED="a#b"` + "\n"
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	values, err := ReadEnvValues(dir)
	if err != nil {
		t.Fatalf("ReadEnvValues: %v", err)
	}

	cases := map[string]string{
		"foo":            "bar",     // 空白之后的 # 是行内注释
		"bar":            "foo#baz", // # 前无空白，值原样保留（与 godotenv TestComments 一致）
		"baz":            "foo",     // 引号收尾后紧跟 # 也不进入值
		"COMMENTED":      "foo",     // 验收用例 bar=foo #baz 的值形态
		"REDIS_PASSWORD": "pa#ss",   // 未加引号的 # 不再被误截
		"QUOTED":         "a#b",     // 引号包裹的 # 行为保持不变
	}
	for key, want := range cases {
		if got := values[key]; got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
}

// TestImageWarningsOnParseFailure 回归 P4-10①：
// image.yaml 解析失败时整份配置都不生效，必须被记录下来，
// 以便 app.NewServer 在日志系统就绪后以 Error 级再次输出（GUI 无控制台，
// 只看 stderr 等于用户完全不知情）。
func TestImageWarningsOnParseFailure(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	// 故意写坏：default_category 需要列表，标量会让整个文档解析失败
	broken := "external_apis:\n  - name: x\n    url: https://e.com/{width}\n    default_category: scenery\n"
	if err := os.WriteFile(filepath.Join(dir, "config", "image.yaml"), []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Load(dir); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(ImageWarnings) == 0 {
		t.Fatal("ImageWarnings 为空：解析失败未被记录（GUI 用户将毫无线索）")
	}
	if len(Image.ExternalAPIs) != 0 {
		t.Errorf("解析失败时不应有生效的 API，got %d", len(Image.ExternalAPIs))
	}

	// 正常配置时不应产生告警（避免误报）
	okDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(okDir, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	good := "external_apis:\n  - name: x\n    url: https://e.com/{width}\n    default_category: [scenery]\n"
	if err := os.WriteFile(filepath.Join(okDir, "config", "image.yaml"), []byte(good), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Load(okDir); err != nil {
		t.Fatalf("Load(ok): %v", err)
	}
	if len(ImageWarnings) != 0 {
		t.Errorf("正常配置却产生告警: %v", ImageWarnings)
	}
	if len(Image.ExternalAPIs) != 1 {
		t.Errorf("正常配置应解析出 1 个 API，got %d", len(Image.ExternalAPIs))
	}
}

// TestLogLevelNormalization 回归 P5-5：
// LOG_LEVEL 值大小写不敏感（与"配置项大小写不敏感"的声明一致），
// 非法值回退 info 而不是静默保留。
func TestLogLevelNormalization(t *testing.T) {
	cases := []struct{ in, want string }{
		{"debug", "debug"},
		{"DEBUG", "debug"},
		{"Debug", "debug"},
		{"  WARN  ", "warn"},
		{"Error", "error"},
		{"info", "info"},
		{"verbose", "info"}, // 非法值 → 回退默认
		{"", "info"},        // 空值 → getStr 取默认 "info"，同样通过
	}
	for _, c := range cases {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, ".env"),
			[]byte("LOG_LEVEL="+c.in+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := Load(dir); err != nil {
			t.Fatalf("Load(%q): %v", c.in, err)
		}
		if C.LogLevel != c.want {
			t.Errorf("LOG_LEVEL=%q → %q, want %q", c.in, C.LogLevel, c.want)
		}
	}
}

// TestUpdateEnvFileHandlesExportBOMAndQuoting 回归 R5-16：
// shell 风格的 export 前缀、UTF-8 BOM、以及需要转义的值（含 # / $）
// 在 GUI 回写时都不能被破坏（不产生重复键、值不被截断或展开）。
func TestUpdateEnvFileHandlesExportBOMAndQuoting(t *testing.T) {
	dir := t.TempDir()
	content := "\ufeffexport APP_PORT=9090\n# 保留注释\nREFERER_WHITELIST=\n"
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := UpdateEnvFile(dir, map[string]string{
		"APP_PORT":          "8081",
		"REFERER_WHITELIST": "a.com#b", // 含 #：必须加引号，否则回读被截断
	}); err != nil {
		t.Fatalf("UpdateEnvFile: %v", err)
	}

	values, err := ReadEnvValues(dir)
	if err != nil {
		t.Fatalf("ReadEnvValues: %v", err)
	}
	if got := values["APP_PORT"]; got != "8081" {
		t.Errorf("APP_PORT = %q, want 8081（export 前缀需正确识别）", got)
	}
	if got := values["REFERER_WHITELIST"]; got != "a.com#b" {
		t.Errorf("REFERER_WHITELIST = %q, want a.com#b（含 # 的值需加引号）", got)
	}

	raw, err := os.ReadFile(filepath.Join(dir, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(raw), "APP_PORT="); n != 1 {
		t.Fatalf("APP_PORT 出现 %d 次，应为 1（不得追加重复键）\n%s", n, raw)
	}
	if strings.HasPrefix(string(raw), "\ufeff") {
		t.Error("回写后应去掉 BOM（否则首行键名带 \\ufeff，godotenv 读不到）")
	}
	if !strings.Contains(string(raw), "# 保留注释") {
		t.Error("无关的注释行应被保留")
	}
}

// TestLoadIgnoresEmptyEnvValues 回归 R5-17：
// 空环境变量视为"未设置"，不能让 APP_HOST= 把服务暴露到 0.0.0.0。
func TestLoadIgnoresEmptyEnvValues(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"),
		[]byte("APP_HOST=127.0.0.1\nAPP_PORT=9090\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 模拟 Docker Compose 的 `APP_HOST:` —— 注入空字符串
	t.Setenv("APP_HOST", "")
	t.Setenv("APP_PORT", "")

	if err := Load(dir); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if C.Host != "127.0.0.1" {
		t.Errorf("Host = %q, want 127.0.0.1（空环境变量不应覆盖 .env）", C.Host)
	}
	if C.Port != 9090 {
		t.Errorf("Port = %d, want 9090（空环境变量不应覆盖 .env）", C.Port)
	}
}

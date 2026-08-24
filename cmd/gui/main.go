//go:build windows

// img-api-gui — Windows 桌面控制面板。
//
// 为不熟悉命令行的用户提供图形界面：
//   - 启动 / 停止 / 重启服务
//   - 核心设置编辑（保存后自动重启服务生效）
//   - 开机自启（当前用户级注册表，无需管理员）
//   - 后台运行（关闭窗口最小化到托盘，服务继续运行）
//
// 命令行用户请直接使用 img-api.exe，二者互不干扰。
package main

import (
	"context"
	_ "embed" // 供 go:embed 指令使用
	"errors"
	"flag"
	"fmt"
	"image/color"
	"net/url"
	"os"
	"strconv"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	imgapp "img-api/internal/app"
	"img-api/internal/config"
)

var (
	srv   *imgapp.Server
	srvMu sync.Mutex
)

//go:embed icon.png
var iconPNG []byte

func main() {
	background := flag.Bool("background", false, "start minimized to system tray")
	flag.Parse()

	a := app.NewWithID("com.github.chihirosyd.img-api")
	a.SetIcon(fyne.NewStaticResource("img-api.png", iconPNG))
	a.Settings().SetTheme(newBrandTheme(a.Preferences().Bool("dark")))
	w := a.NewWindow("img-api 控制面板")

	rootPath := imgapp.RootPath()

	// ── 状态指示（彩色圆点 + 状态文本 + 首页地址，hero 渐变内白色文字）──
	dotGreen := color.NRGBA{R: 0x2d, G: 0xa4, B: 0x4e, A: 0xff}
	dotGray := color.NRGBA{R: 0x9c, G: 0xa3, B: 0xaf, A: 0xff}
	heroWhite := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	heroDim := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xb0} // 副文字 69% 白
	statusDot := canvas.NewCircle(dotGray)
	statusLabel := canvas.NewText("服务已停止", heroWhite)
	statusLabel.TextSize = 16
	statusLabel.TextStyle = fyne.TextStyle{Bold: true}
	urlLabel := canvas.NewText("http://127.0.0.1:8080", heroDim)
	urlLabel.TextSize = 12
	versionLabel := widget.NewLabel("img-api")
	refreshStatus := func() {
		if srv != nil && srv.Running() {
			statusDot.FillColor = dotGreen
			statusLabel.Text = fmt.Sprintf("运行中 · 端口 %d · v%s", srv.Port(), config.C.Version)
			urlLabel.Text = fmt.Sprintf("http://127.0.0.1:%d", srv.Port())
			versionLabel.SetText(fmt.Sprintf("img-api v%s", config.C.Version))
		} else {
			statusDot.FillColor = dotGray
			statusLabel.Text = "服务已停止"
			urlLabel.Text = fmt.Sprintf("http://127.0.0.1:%d", config.C.Port)
			versionLabel.SetText(fmt.Sprintf("img-api v%s", config.C.Version))
		}
		statusDot.Refresh()
		statusLabel.Refresh()
		urlLabel.Refresh()
	}

	// ── 操作反馈（横幅提示，2.5 秒后自动消失；序号防止旧任务清掉新消息）──
	flashLabel := widget.NewLabel("")
	flashLabel.TextStyle = fyne.TextStyle{Bold: true}
	var flashSeq int
	flash := func(msg string) {
		flashSeq++
		seq := flashSeq
		flashLabel.SetText(msg)
		go func() {
			time.Sleep(2500 * time.Millisecond)
			fyne.Do(func() {
				if flashSeq == seq {
					flashLabel.SetText("")
				}
			})
		}()
	}

	// ── 服务控制 ──
	startSrv := func() error {
		srvMu.Lock()
		defer srvMu.Unlock()
		if srv != nil && srv.Running() {
			return nil
		}
		s, err := imgapp.NewServer(rootPath)
		if err != nil {
			return err
		}
		if err := s.Start(); err != nil {
			return err
		}
		srv = s
		return nil
	}
	stopSrv := func() {
		srvMu.Lock()
		defer srvMu.Unlock()
		if srv == nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}

	startBtn := widget.NewButtonWithIcon("启动服务", theme.MediaPlayIcon(), func() {
		if err := startSrv(); err != nil {
			showError(fmt.Sprintf("启动失败：%v\n（若命令行版 img-api.exe 或其他程序已在运行，请先关闭）", err), w)
			refreshStatus()
			return
		}
		flash("✅ 服务已启动")
		refreshStatus()
	})
	startBtn.Importance = widget.HighImportance
	stopBtn := widget.NewButtonWithIcon("停止服务", theme.MediaStopIcon(), func() {
		stopSrv()
		flash("⏹ 服务已停止")
		refreshStatus()
	})
	stopBtn.Importance = widget.DangerImportance
	restartBtn := widget.NewButtonWithIcon("重启服务", theme.ViewRefreshIcon(), func() {
		stopSrv()
		if err := startSrv(); err != nil {
			showError(fmt.Sprintf("重启失败：%v", err), w)
		} else {
			flash("🔄 服务已重启")
		}
		refreshStatus()
	})
	restartBtn.Importance = widget.WarningImportance
	homeBtn := widget.NewButtonWithIcon("打开首页", theme.HomeIcon(), func() {
		port := config.C.Port
		if srv != nil && srv.Running() {
			port = srv.Port()
		}
		imgapp.OpenHome(port)
	})
	homeBtn.Importance = widget.LowImportance
	dirBtn := widget.NewButtonWithIcon("配置目录", theme.FolderOpenIcon(), func() {
		imgapp.OpenURL("file://" + rootPath)
	})
	dirBtn.Importance = widget.LowImportance

	// ── 设置表单（核心 6 项）──
	values, _ := config.ReadEnvValues(rootPath)

	portEntry := widget.NewEntry()
	portEntry.SetText(getDefault(values, "APP_PORT", "8080"))
	debugCheck := widget.NewCheck("", nil)
	debugCheck.SetChecked(getDefault(values, "APP_DEBUG", "false") == "true")
	sourceSelect := widget.NewSelect([]string{"txt", "local", "external"}, nil)
	sourceSelect.SetSelected(getDefault(values, "DEFAULT_SOURCE", "txt"))
	refererEntry := widget.NewEntry()
	refererEntry.SetText(getDefault(values, "REFERER_WHITELIST", ""))
	rateCheck := widget.NewCheck("", nil)
	rateCheck.SetChecked(getDefault(values, "RATE_LIMIT_ENABLED", "true") == "true")
	rateMaxEntry := widget.NewEntry()
	rateMaxEntry.SetText(getDefault(values, "RATE_LIMIT_MAX", "60"))

	form := &widget.Form{
		Items: []*widget.FormItem{
			{Text: "监听端口", Widget: portEntry, HintText: "修改后需重启生效"},
			{Text: "调试模式", Widget: debugCheck, HintText: "生产环境建议关闭"},
			{Text: "默认图源", Widget: sourceSelect},
			{Text: "防盗链白名单", Widget: refererEntry, HintText: "逗号分隔域名，留空不限制"},
			{Text: "IP 限流", Widget: rateCheck},
			{Text: "每分钟每 IP 上限", Widget: rateMaxEntry},
		},
	}

	saveBtn := widget.NewButtonWithIcon("保存设置并重启", theme.ConfirmIcon(), func() {
		// 数值校验
		if _, err := strconv.Atoi(portEntry.Text); err != nil {
			showError(fmt.Sprintf("端口必须是数字：%q", portEntry.Text), w)
			return
		}
		if _, err := strconv.Atoi(rateMaxEntry.Text); err != nil {
			showError(fmt.Sprintf("限流阈值必须是数字：%q", rateMaxEntry.Text), w)
			return
		}
		updates := map[string]string{
			"APP_PORT":           portEntry.Text,
			"APP_DEBUG":          strconv.FormatBool(debugCheck.Checked),
			"DEFAULT_SOURCE":     sourceSelect.Selected,
			"REFERER_WHITELIST":  refererEntry.Text,
			"RATE_LIMIT_ENABLED": strconv.FormatBool(rateCheck.Checked),
			"RATE_LIMIT_MAX":     rateMaxEntry.Text,
		}
		if err := config.UpdateEnvFile(rootPath, updates); err != nil {
			showError(fmt.Sprintf("保存配置失败：%v", err), w)
			return
		}
		stopSrv()
		if err := startSrv(); err != nil {
			showError(fmt.Sprintf("重启失败：%v", err), w)
		}
		refreshStatus()
		flash("✅ 设置已保存并重启")
		showInfo("已保存", "设置已写入 .env 并重启服务", w)
	})
	saveBtn.Importance = widget.HighImportance

	// ── 系统选项 ──
	autoStartCheck := widget.NewCheck("开机自启（当前用户，无需管理员）", nil)
	autoStartCheck.OnChanged = func(checked bool) {
		exe, err := os.Executable()
		if err != nil {
			showError(fmt.Sprintf("无法获取程序路径：%v", err), w)
			autoStartCheck.SetChecked(!checked)
			return
		}
		if err := imgapp.SetAutoStart(checked, exe, "--background"); err != nil {
			showError(fmt.Sprintf("设置开机自启失败：%v", err), w)
			autoStartCheck.SetChecked(!checked)
		} else if checked {
			flash("✅ 已开启开机自启（后台静默运行）")
		} else {
			flash("已关闭开机自启")
		}
	}
	trayCheck := widget.NewCheck("后台运行（关闭窗口时最小化到托盘）", nil)
	darkCheck := widget.NewCheck("深色模式", func(checked bool) {
		a.Settings().SetTheme(newBrandTheme(checked))
		a.Preferences().SetBool("dark", checked)
	})
	darkCheck.SetChecked(a.Preferences().Bool("dark"))

	// ── 布局（渐变 hero + 卡片式分区 + 操作反馈条）──
	dotBox := container.NewGridWrap(fyne.NewSize(14, 14), statusDot)
	heroGrad := canvas.NewLinearGradient(
		color.NRGBA{R: 0x3d, G: 0x8b, B: 0xfd, A: 0xff},
		color.NRGBA{R: 0x0d, G: 0x4a, B: 0x92, A: 0xff},
		90,
	)
	heroTitle := canvas.NewText("img-api 控制面板", heroWhite)
	heroTitle.TextSize = 20
	heroTitle.TextStyle = fyne.TextStyle{Bold: true}
	heroSub := canvas.NewText("随机图片 API · 启动 / 停止 / 设置 / 自启，全在这一个窗口", heroDim)
	heroSub.TextSize = 12
	hero := container.NewStack(
		heroGrad,
		container.NewPadded(container.NewVBox(
			container.NewBorder(nil, nil, nil, container.NewHBox(homeBtn, dirBtn),
				container.NewVBox(heroTitle, heroSub)),
			container.NewHBox(dotBox, statusLabel),
			urlLabel,
		)),
	)
	ctrlCard := widget.NewCard("服务控制", "启动 / 停止 / 重启本机服务",
		container.NewGridWithColumns(3, startBtn, stopBtn, restartBtn))
	settingsCard := widget.NewCard("设置", "保存后自动重启服务生效",
		container.NewVBox(form, saveBtn))
	systemCard := widget.NewCard("系统", "开机自启 · 后台运行 · 外观",
		container.NewVBox(autoStartCheck, trayCheck, darkCheck))

	ghURL, _ := url.Parse("https://github.com/chihirosyd/img-api")
	footer := container.NewBorder(nil, nil, nil,
		widget.NewHyperlink("GitHub 仓库 ↗", ghURL), versionLabel)

	content := container.NewPadded(container.NewVBox(
		hero, flashLabel, ctrlCard, settingsCard, systemCard, footer))
	w.SetContent(container.NewVScroll(content))
	w.Resize(fyne.NewSize(560, 700))

	// ── 托盘菜单 ──
	showItem := fyne.NewMenuItem("显示窗口", func() { w.Show() })
	quitItem := fyne.NewMenuItem("退出（停止服务）", func() {
		stopSrv()
		a.Quit()
	})
	trayMenu := fyne.NewMenu("img-api", showItem, quitItem)
	// Fyne v2.8 中 SetSystemTrayMenu 不在 fyne.App 接口里，
	// 仅桌面驱动实现提供，经接口断言调用；不支持的平台静默跳过（无托盘）。
	if trayApp, ok := a.(interface{ SetSystemTrayMenu(*fyne.Menu) }); ok {
		trayApp.SetSystemTrayMenu(trayMenu)
	}

	// ── 关闭行为：后台运行 → 隐藏到托盘；否则退出并停止服务 ──
	w.SetCloseIntercept(func() {
		if trayCheck.Checked {
			w.Hide()
			return
		}
		stopSrv()
		a.Quit()
	})

	// ── 启动即运行服务 ──
	if err := startSrv(); err != nil {
		showError(fmt.Sprintf("启动失败：%v", err), w)
	}
	refreshStatus()

	// --background（开机自启）时窗口隐藏，但仍需进入事件循环保持托盘与进程存活。
	if *background {
		w.Hide()
	}
	w.ShowAndRun()
}

// getDefault 从 env map 取值，缺失时返回默认值。
func getDefault(m map[string]string, key, def string) string {
	if v, ok := m[key]; ok && v != "" {
		return v
	}
	return def
}

// showInfo 弹出自定义按钮文字的提示框。
func showInfo(title, msg string, w fyne.Window) {
	d := dialog.NewInformation(title, msg, w)
	d.SetDismissText("知道了")
	d.Show()
}

// showError 弹出自定义按钮文字的错误框。
func showError(msg string, w fyne.Window) {
	d := dialog.NewError(errors.New(msg), w)
	d.SetDismissText("关闭")
	d.Show()
}

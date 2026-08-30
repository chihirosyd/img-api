//go:build windows

// brandTheme 为 GUI 提供与网页首页一致的品牌配色（品牌蓝 #2f81f7），
// 在 Fyne 内置亮色/暗色主题基础上覆盖主色、链接色、焦点色与选区色。
package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

var (
	// brandBlue 品牌主色，与网页首页渐变主色一致。
	brandBlue = color.NRGBA{R: 0x2f, G: 0x81, B: 0xf7, A: 0xff}
	// brandBlueBright 暗色模式下的品牌亮蓝，对比度更佳。
	brandBlueBright = color.NRGBA{R: 0x7d, G: 0xb3, B: 0xff, A: 0xff}
)

// brandTheme 嵌入内置主题，只覆盖品牌相关颜色，其余颜色/字体/尺寸沿用默认。
type brandTheme struct {
	fyne.Theme
	dark bool
}

// newBrandTheme 返回亮色或暗色的品牌主题。
//
// 注意：theme.LightTheme()/DarkTheme() 已弃用（v3 将移除），
// 这里基于 DefaultTheme，并在 Color 中显式按开关选择亮/暗变体，
// 使全部颜色（含非品牌色）与 GUI 深色模式开关保持一致。
func newBrandTheme(dark bool) fyne.Theme {
	return &brandTheme{Theme: theme.DefaultTheme(), dark: dark}
}

func (t *brandTheme) Color(name fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNamePrimary, theme.ColorNameHyperlink, theme.ColorNameFocus:
		if t.dark {
			return brandBlueBright
		}
		return brandBlue
	case theme.ColorNameSelection:
		if t.dark {
			return color.NRGBA{R: 0x24, G: 0x4c, B: 0x8f, A: 0xff}
		}
		return color.NRGBA{R: 0xd7, G: 0xe7, B: 0xfe, A: 0xff}
	case theme.ColorNameHover:
		// 悬停反馈：浅品牌蓝（按钮/列表项悬停时更明显）
		if t.dark {
			return color.NRGBA{R: 0x2b, G: 0x34, B: 0x46, A: 0xff}
		}
		return color.NRGBA{R: 0xe3, G: 0xef, B: 0xfe, A: 0xff}
	case theme.ColorNamePressed:
		// 按压反馈：比悬停更深一档
		if t.dark {
			return color.NRGBA{R: 0x36, G: 0x42, B: 0x58, A: 0xff}
		}
		return color.NRGBA{R: 0xcf, G: 0xe2, B: 0xfd, A: 0xff}
	}
	// 非品牌色：忽略渲染器传入的系统偏好变体，显式按开关取值
	variant := theme.VariantLight
	if t.dark {
		variant = theme.VariantDark
	}
	return t.Theme.Color(name, variant)
}

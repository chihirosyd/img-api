// genicon 生成 img-api 的图标资源：
//   - cmd/gui/icon.png  256×256 PNG（GUI 窗口/托盘图标，运行时 embed）
//   - cmd/gui/icon.ico  多尺寸 ICO（exe 文件图标，配合已提交的 icon_windows.syso
//     在 Windows 构建时生效；重新生成 ico 后需 windres 重建 syso）
//
// 图标为品牌蓝渐变圆角底 + 白色山峦与太阳（照片风格，贴合"图片 API"定位）。
//
// 用法（在项目根目录）：
//
//	go run ./cmd/genicon
package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"log"
	"os"
	"path/filepath"
)

var (
	bgTop    = color.RGBA{R: 0x4a, G: 0x9c, B: 0xf5, A: 0xff} // 渐变起点（浅品牌蓝）
	bgBottom = color.RGBA{R: 0x0d, G: 0x4a, B: 0x92, A: 0xff} // 渐变终点（深品牌蓝）
	white    = color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
)

func main() {
	outDir := filepath.Join("cmd", "gui")
	writePNG(filepath.Join(outDir, "icon.png"), 256)
	if err := writeICO(filepath.Join(outDir, "icon.ico"),
		[]int{16, 24, 32, 48, 64, 128, 256}); err != nil {
		log.Fatal(err)
	}
	log.Println("generated:", filepath.Join(outDir, "icon.png"), "and", filepath.Join(outDir, "icon.ico"))
}

// draw 绘制尺寸为 size 的图标画布。
func draw(size int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	radius := int(float64(size) * 0.19) // 圆角半径

	// 太阳：圆心与半径
	cx, cy := float64(size)*0.69, float64(size)*0.31
	sunR := float64(size) * 0.125

	// 山峦（两座白色三角形）
	mnt1 := [3][2]float64{{0.14, 0.78}, {0.50, 0.28}, {0.70, 0.78}}
	mnt2 := [3][2]float64{{0.42, 0.78}, {0.86, 0.42}, {0.94, 0.78}}

	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			if !inRoundedRect(x, y, size, radius) {
				continue // 圆角外透明
			}
			// 垂直渐变背景
			t := float64(y) / float64(size-1)
			c := lerp(bgTop, bgBottom, t)
			img.SetRGBA(x, y, c)

			px, py := float64(x), float64(y)
			if inTriangle(px, py, mnt1) || inTriangle(px, py, mnt2) {
				img.SetRGBA(x, y, white)
				continue
			}
			// 太阳（绘制在山之上）
			dx, dy := px-cx, py-cy
			if dx*dx+dy*dy <= sunR*sunR {
				img.SetRGBA(x, y, white)
			}
		}
	}
	return img
}

// inRoundedRect 判断像素是否落在圆角矩形内部（含圆角）。
func inRoundedRect(x, y, size, r int) bool {
	if x < 0 || y < 0 || x >= size || y >= size {
		return false
	}
	if x < r && y < r {
		return sq(x-r)+sq(y-r) <= r*r
	}
	if x >= size-r && y < r {
		return sq(x-(size-1-r))+sq(y-r) <= r*r
	}
	if x < r && y >= size-r {
		return sq(x-r)+sq(y-(size-1-r)) <= r*r
	}
	if x >= size-r && y >= size-r {
		return sq(x-(size-1-r))+sq(y-(size-1-r)) <= r*r
	}
	return true
}

func sq(v int) int { return v * v }

// inTriangle 重心法判断点是否在三角形内。
func inTriangle(px, py float64, t [3][2]float64) bool {
	d1 := sign(px, py, t[0], t[1])
	d2 := sign(px, py, t[1], t[2])
	d3 := sign(px, py, t[2], t[0])
	neg := d1 < 0 || d2 < 0 || d3 < 0
	pos := d1 > 0 || d2 > 0 || d3 > 0
	return !(neg && pos)
}

func sign(px, py float64, a, b [2]float64) float64 {
	return (px-b[0])*(a[1]-b[1]) - (a[0]-b[0])*(py-b[1])
}

func lerp(a, b color.RGBA, t float64) color.RGBA {
	return color.RGBA{
		R: uint8(float64(a.R) + (float64(b.R)-float64(a.R))*t + 0.5),
		G: uint8(float64(a.G) + (float64(b.G)-float64(a.G))*t + 0.5),
		B: uint8(float64(a.B) + (float64(b.B)-float64(a.B))*t + 0.5),
		A: 0xff,
	}
}

// writePNG 输出单个尺寸的 PNG。
func writePNG(path string, size int) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, draw(size)); err != nil {
		log.Fatal(err)
	}
	if err := writeFile(path, buf.Bytes()); err != nil {
		log.Fatal(err)
	}
}

// writeICO 输出多尺寸 ICO（各尺寸条目使用 PNG 压缩，Vista+ 支持）。
func writeICO(path string, sizes []int) error {
	var images [][]byte
	for _, s := range sizes {
		var buf bytes.Buffer
		if err := png.Encode(&buf, draw(s)); err != nil {
			return err
		}
		images = append(images, buf.Bytes())
	}

	var out bytes.Buffer
	// ICONDIR
	binary.Write(&out, binary.LittleEndian, uint16(0)) // reserved
	binary.Write(&out, binary.LittleEndian, uint16(1)) // type: icon
	binary.Write(&out, binary.LittleEndian, uint16(len(sizes)))

	offset := 6 + 16*len(sizes)
	for i, s := range sizes {
		b := byte(s)
		if s >= 256 {
			b = 0 // 256 用 0 表示
		}
		binary.Write(&out, binary.LittleEndian, b)         // width
		binary.Write(&out, binary.LittleEndian, b)         // height
		binary.Write(&out, binary.LittleEndian, uint8(0))  // colors
		binary.Write(&out, binary.LittleEndian, uint8(0))  // reserved
		binary.Write(&out, binary.LittleEndian, uint16(1)) // planes
		binary.Write(&out, binary.LittleEndian, uint16(32))
		binary.Write(&out, binary.LittleEndian, uint32(len(images[i])))
		binary.Write(&out, binary.LittleEndian, uint32(offset))
		offset += len(images[i])
	}
	for _, img := range images {
		out.Write(img)
	}
	return writeFile(path, out.Bytes())
}

func writeFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0o644)
}

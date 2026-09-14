package service

// 本文件集中图库目录/分类的探测辅助函数，供提示页判定与分类清单快照使用
// （SourceEmpty / CategoryExists / CategoryExistsFor / AvailableCategories）。
// 解析规则与 TxtRepository / LocalRepository 保持一致。

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"img-api/internal/model"
	"img-api/internal/repository"
)

// txtLineValid 判定一行是否有效图片 URL：非空、非注释、且为 http/https
// （与 repository.ReadTxtLines 的过滤规则一致）。
func txtLineValid(line string) bool {
	return line != "" && !strings.HasPrefix(line, "#") && repository.IsValidImageURL(line)
}

// txtDirHasContent 检查 TXT 图库目录下是否存在含有效 URL 的 .txt 文件。
// 有效 URL：非空行且不以 # 开头（与 TxtRepository.readLines 规则一致）。
func txtDirHasContent(root string) bool {
	for _, dev := range []string{"pc", "pe"} {
		entries, err := os.ReadDir(filepath.Join(root, dev))
		if err != nil {
			continue // 目录不存在则跳过
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".txt") {
				continue
			}
			f, err := os.Open(filepath.Join(root, dev, e.Name()))
			if err != nil {
				continue
			}
			hasURL := false
			scanner := bufio.NewScanner(f)
			for scanner.Scan() {
				if txtLineValid(strings.TrimSpace(scanner.Text())) {
					hasURL = true
					break
				}
			}
			// 超长行（>64KB）视为"有内容"：读取路径（ReadTxtLines）会显式报错，
			// 两条路径结论一致，避免此处判"无内容"→404、读取处→500 的矛盾
			if !hasURL && errors.Is(scanner.Err(), bufio.ErrTooLong) {
				hasURL = true
			}
			f.Close()
			if hasURL {
				return true
			}
		}
	}
	return false
}

// localDirHasImages 检查本地图片目录下是否存在任意图片文件。
func localDirHasImages(root string) bool {
	for _, dev := range []string{"pc", "pe"} {
		found := false
		_ = filepath.WalkDir(filepath.Join(root, dev), func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if model.ImageExts[strings.ToLower(filepath.Ext(d.Name()))] {
				found = true
				return filepath.SkipAll // 找到一个即可停止扫描
			}
			return nil
		})
		if found {
			return true
		}
	}
	return false
}

// txtCategoryExistsForDevice 检查指定设备目录（pc/pe）下某分类的 TXT 文件
// 是否含有效 URL。供 CategoryExistsFor 与分类清单快照按设备精确判定使用。
func txtCategoryExistsForDevice(root, device, category string) bool {
	path := filepath.Join(root, device, category+".txt")
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	hasURL := false
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if txtLineValid(strings.TrimSpace(scanner.Text())) {
			hasURL = true
			break
		}
	}
	// 超长行视为"有内容"（与 txtDirHasContent 一致）
	if !hasURL && errors.Is(scanner.Err(), bufio.ErrTooLong) {
		hasURL = true
	}
	return hasURL
}

// txtCategoriesForDevice 列出指定设备目录下含有效 URL 的分类名（保持目录顺序）。
// 内容校验与 txtCategoryExistsForDevice 一致：仅注释/空文件不算存在。
func txtCategoriesForDevice(root, device string) []string {
	entries, err := os.ReadDir(filepath.Join(root, device))
	if err != nil {
		return nil
	}
	var result []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".txt") {
			continue
		}
		// 分类文件名统一按小写处理：英文字母归一化为小写（避免 ANIME.TXT 与
		// anime.txt 被视为不同分类）；中文无大小写，ToLower 原样保留
		name := strings.TrimSuffix(strings.ToLower(e.Name()), ".txt")
		if txtCategoryExistsForDevice(root, device, name) {
			result = append(result, name)
		}
	}
	return result
}

// localCategoryExistsForDevice 检查指定设备目录（pc/pe）下某分类是否包含图片。
// 供 CategoryExistsFor 与分类清单快照按设备精确判定使用。
func localCategoryExistsForDevice(root, device, category string) bool {
	dir := filepath.Join(root, device, category)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && model.ImageExts[strings.ToLower(filepath.Ext(e.Name()))] {
			return true
		}
	}
	return false
}

// localCategoriesForDevice 列出指定设备目录下含图片的分类目录名（保持目录顺序）。
// 内容校验与 localCategoryExistsForDevice 一致：目录下直接含图片文件才算存在。
func localCategoriesForDevice(root, device string) []string {
	entries, err := os.ReadDir(filepath.Join(root, device))
	if err != nil {
		return nil
	}
	var result []string
	for _, e := range entries {
		if e.IsDir() && localCategoryExistsForDevice(root, device, e.Name()) {
			result = append(result, e.Name())
		}
	}
	return result
}

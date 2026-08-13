package service

// 本文件集中图库目录/分类的探测辅助函数，供提示页判定使用
// （SourceEmpty / CategoryExists / AvailableCategories）。
// 解析规则与 TxtRepository / LocalRepository 保持一致。

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"img-api/internal/model"
)

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
				line := strings.TrimSpace(scanner.Text())
				if line != "" && !strings.HasPrefix(line, "#") {
					hasURL = true
					break
				}
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

// txtCategoryExists 检查 TXT 图库中某分类是否存在含有效 URL 的文件。
// 检查 pc/pe 两个设备目录，任一存在即 true。
func txtCategoryExists(root, category string) bool {
	for _, dev := range []string{"pc", "pe"} {
		if txtCategoryExistsForDevice(root, dev, category) {
			return true
		}
	}
	return false
}

// txtCategoryExistsForDevice 检查指定设备目录（pc/pe）下某分类的 TXT 文件
// 是否含有效 URL。供 CategoryExistsFor 按设备精确判定使用。
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
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			hasURL = true
			break
		}
	}
	return hasURL
}

// txtCategories 列出 TXT 图库的所有分类名（去重，pc/pe 合并）。
func txtCategories(root string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, dev := range []string{"pc", "pe"} {
		entries, err := os.ReadDir(filepath.Join(root, dev))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".txt") {
				continue
			}
			name := strings.TrimSuffix(e.Name(), ".txt")
			if !seen[name] {
				seen[name] = true
				result = append(result, name)
			}
		}
	}
	return result
}

// localCategoryExists 检查本地图片中某分类目录是否包含图片。
// 检查 pc/pe 两个设备目录，任一存在即 true。
func localCategoryExists(root, category string) bool {
	for _, dev := range []string{"pc", "pe"} {
		if localCategoryExistsForDevice(root, dev, category) {
			return true
		}
	}
	return false
}

// localCategoryExistsForDevice 检查指定设备目录（pc/pe）下某分类是否包含图片。
// 供 CategoryExistsFor 按设备精确判定使用。
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

// localCategories 列出本地图片的所有分类目录名（去重，pc/pe 合并）。
func localCategories(root string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, dev := range []string{"pc", "pe"} {
		entries, err := os.ReadDir(filepath.Join(root, dev))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() && !seen[e.Name()] {
				seen[e.Name()] = true
				result = append(result, e.Name())
			}
		}
	}
	return result
}

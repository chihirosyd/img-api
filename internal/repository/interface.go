// Package repository 定义图片仓库的抽象接口及三种内置实现。
//
// 设计原则（开闭原则）：
//   - ImageRepository 接口定义契约
//   - TXT / Local / External 三种实现各司其职
//   - 新增数据源只需实现接口 + 在 SourceByName 中注册
//   - handler/service 层无需任何修改
package repository

import (
	"context"
	"path/filepath"

	"img-api/internal/cache"
	"img-api/internal/model"
)

// ImageRepository 是图片仓库的抽象接口。
//
// 所有数据源（TXT 文件 / 本地磁盘 / 远端 API）都必须实现此接口。
// Service 层依赖此接口而非具体实现，实现了解耦。
type ImageRepository interface {
	// Name 返回仓库的人类可读名称（如 "txt"、"local"、"external"）。
	// 主要用于日志和 Health 响应中的 key。
	Name() string

	// Random 从仓库中随机获取一张图片。
	//
	// 参数：
	//   ctx       — 用于超时控制和取消传播
	//   category  — 图片分类名（"" 等价于 "default"）
	//   deviceType — PC 或 PE，决定从哪个设备子目录读取
	//
	// 返回 Image{URL, Category} 或 ErrNoImage。
	Random(ctx context.Context, category string, deviceType model.DeviceType) (*model.Image, error)

	// Health 检查仓库当前是否可用。
	// nil = 健康，非 nil = 不可用（含具体原因）。
	Health(ctx context.Context) error
}

// SourceByName 是仓库工厂函数，根据来源名称返回对应实现。
//
// cache 用于 TxtRepository 的 Redis Set 加速（可为 nil）。
// externalPool 仅在 source=external 时需要。
// 未知来源默认回退到 TxtRepository。
func SourceByName(name model.SourceType, rootPath string, externalPool *ExternalPool, c cache.Cache) ImageRepository {
	switch name {
	case model.SourceTxt:
		return NewTxtRepository(filepath.Join(rootPath, "resources", "txt"), c)
	case model.SourceLocal:
		return NewLocalRepository(
			filepath.Join(rootPath, "resources", "local"),
			filepath.Join(rootPath, "storage", "index", "local.json"),
		)
	case model.SourceExternal:
		return NewExternalRepository(externalPool)
	default:
		return NewTxtRepository(filepath.Join(rootPath, "resources", "txt"), c) // 兜底
	}
}

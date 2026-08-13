package repository

import (
	"context"

	"img-api/internal/model"
)

// ExternalRepository 适配 ExternalPool 以符合 ImageRepository 接口。
//
// 与 TxtRepository / LocalRepository 不同，ExternalRepository 不直接
// 访问外部 API，而是委托给 ExternalPool 完成实际的 HTTP 请求。
// 熔断保护在 Service 层处理。
type ExternalRepository struct {
	pool *ExternalPool // 外部 API 池实例（nil 表示不可用）
}

// NewExternalRepository 创建适配器。pool 为 nil 时 Random 会返回错误。
func NewExternalRepository(pool *ExternalPool) *ExternalRepository {
	return &ExternalRepository{pool: pool}
}

// Name 实现 ImageRepository 接口。
func (r *ExternalRepository) Name() string { return "external" }

// Random 从外部 API 池随机选取一个 API 并获取图片。
// category 用于筛选支持该分类的 API 端点（见 ExternalPool.filterByCategory）。
// pool 为空时返回 ErrNoImage。注意：Service 层实际通过熔断器直接调用
// ExternalPool.Random（见 randomFromExternal），此适配器仅用于满足接口契约。
func (r *ExternalRepository) Random(ctx context.Context, category string, deviceType model.DeviceType) (*model.Image, error) {
	if r.pool == nil {
		return nil, &model.ErrNoImage{Source: "external", Category: category}
	}
	return r.pool.Random(ctx, "", category, deviceType)
}

// Health 检查外部 API 池是否配置了至少一个端点。
func (r *ExternalRepository) Health(ctx context.Context) error {
	if r.pool == nil || len(r.pool.APIs()) == 0 {
		return &model.ErrRepoHealth{Source: "external", Err: nil}
	}
	return nil
}

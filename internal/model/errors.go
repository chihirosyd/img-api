package model

import "fmt"

// ErrNoImage 表示指定来源和分类下没有可用图片。
type ErrNoImage struct {
	Source   string
	Category string
}

func (e *ErrNoImage) Error() string {
	return fmt.Sprintf("no image available: source=%s category=%s", e.Source, e.Category)
}

// ErrAPINotFound 表示请求指定的外部 API 名称在 image.yaml 池中不存在。
// 属于配置错误而非上游故障：Service 层在进入熔断器前提前返回（不计入熔断失败），
// Handler 层据此返回 404 提示页而非 500。
type ErrAPINotFound struct {
	Name string
}

func (e *ErrAPINotFound) Error() string {
	return fmt.Sprintf("external api not found: %s", e.Name)
}

// ErrExternalNotConfigured 表示外部 API 池未配置任何端点。
// 与 ErrNoImage 不同：这是部署配置问题而非图片资源缺失，
// Handler 层据此返回"开始使用"引导页（503）而非普通 500 错误。
// 使用哨兵错误（errors.Is 判断），类似 cache.Nil 的模式。
var ErrExternalNotConfigured = &externalNotConfiguredError{}

type externalNotConfiguredError struct{}

func (e *externalNotConfiguredError) Error() string {
	return "external source not configured: add external_apis entries in configs/image.yaml"
}

// ErrRepoHealth 表示仓库健康检查未通过。
type ErrRepoHealth struct {
	Source string
	Err    error
}

func (e *ErrRepoHealth) Error() string {
	return fmt.Sprintf("repository %s unhealthy: %v", e.Source, e.Err)
}

func (e *ErrRepoHealth) Unwrap() error { return e.Err }

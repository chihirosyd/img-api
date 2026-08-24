// image.yaml（config/image.yaml）的类型定义。
//
// 该文件独立于 .env / 环境变量体系，供外部图片 API 池（repository.ExternalPool）使用。
// 键名大小写不敏感（解析时统一归一化为小写），推荐全小写写法。
package config

import (
	"strings"

	"gopkg.in/yaml.v3"
)

// ExternalAPIConfig 描述一个外部图片 API 端点（从 image.yaml 反序列化）。
type ExternalAPIConfig struct {
	Name            string            `yaml:"name"`             // API 标识
	URL             string            `yaml:"url"`              // 请求模板（{width}/{height}/{category} 占位符）
	Headers         map[string]string `yaml:"headers"`          // 自定义请求头
	ResponseType    string            `yaml:"response_type"`    // redirect / json
	URLField        string            `yaml:"url_field"`        // JSON 中 URL 字段路径
	Categories      []string          `yaml:"categories"`       // 支持的分类（空=匹配所有，["all"]=匹配所有）
	CategoryParam   string            `yaml:"category_param"`   // 分类对应的 query 参数名（如 "query"）
	DefaultCategory []string          `yaml:"default_category"` // 默认分类（多值随机选一；空=回退 "default"）
}

// ImageConfig 对应 config/image.yaml 的顶层结构。
type ImageConfig struct {
	ExternalAPIs []ExternalAPIConfig `yaml:"external_apis"`
}

// UnmarshalYAML 在解码前递归将键名归一化为小写，
// 使键名大小写不敏感（EXTERNAL_APIS / External_Apis 均可识别）。
// 注意：headers 的键（HTTP 头名）也会被转为小写——HTTP 头本身大小写不敏感，无影响。
func (c *ImageConfig) UnmarshalYAML(value *yaml.Node) error {
	normalizeYAMLKeys(value)
	type plain ImageConfig
	var p plain
	if err := value.Decode(&p); err != nil {
		return err
	}
	*c = ImageConfig(p)
	return nil
}

// normalizeYAMLKeys 递归将 mapping 的键转为小写（值原样保留）。
func normalizeYAMLKeys(n *yaml.Node) {
	switch n.Kind {
	case yaml.MappingNode:
		for i := 0; i < len(n.Content); i += 2 {
			n.Content[i].Value = strings.ToLower(n.Content[i].Value)
			normalizeYAMLKeys(n.Content[i+1])
		}
	case yaml.SequenceNode:
		for _, c := range n.Content {
			normalizeYAMLKeys(c)
		}
	}
}

// Image 是 image.yaml 的解析结果（config.Load 填充）。
// 文件缺失或为空时是空配置（外部 API 池为空，source=external 返回引导页）。
var Image *ImageConfig

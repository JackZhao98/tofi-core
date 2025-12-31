package engine

import "tofi-core/internal/models"

type Action interface {
	// Execute 执行节点逻辑
	// config: 经过 ResolveConfig 处理后的局部配置
	// ctx: 全局执行上下文
	Execute(config map[string]interface{}, ctx *models.ExecutionContext) (string, error)

	// Validate 静态验证节点配置
	Validate(node *models.Node) error
}

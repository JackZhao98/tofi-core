package logic

import (
	"fmt"
	"strings"
	"tofi-core/internal/models"
)

type Check struct{}

func (c *Check) Execute(n *models.Node, ctx *models.ExecutionContext) (string, error) {
	// 使用严格模式进行变量替换，字段不存在时会直接报错
	val, err := ctx.ReplaceParamsStrict(fmt.Sprint(n.Input["value"]))
	if err != nil {
		return "", fmt.Errorf("input.value 变量替换失败: %v", err)
	}
	mode := n.Config["mode"] // "is_true", "is_false", "is_empty", "exists"

	var result bool
	switch mode {
	case "is_true":
		result = strings.ToLower(val) == "true" || val == "1"
	case "is_false":
		result = strings.ToLower(val) == "false" || val == "0"
	case "is_empty":
		result = len(strings.TrimSpace(val)) == 0
	case "exists":
		result = len(val) > 0
	default:
		return "", fmt.Errorf("不支持的判定模式: %s", mode)
	}

	if !result {
		// 如果开启了 output_bool 模式，则返回 "false" 而不是报错
		if strings.ToLower(n.Config["output_bool"]) == "true" {
			return "false", nil
		}
		return val, fmt.Errorf("CONDITION_NOT_MET")
	}
	
	if strings.ToLower(n.Config["output_bool"]) == "true" {
		return "true", nil
	}
	return "CHECK_PASSED", nil
}

func (c *Check) Validate(n *models.Node) error {
	if _, ok := n.Input["value"]; !ok {
		return fmt.Errorf("input.value is required")
	}
	mode := n.Config["mode"]
	if mode != "is_true" && mode != "is_false" && mode != "is_empty" && mode != "exists" {
		return fmt.Errorf("invalid config.mode: %s", mode)
	}
	return nil
}

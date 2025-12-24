package logic

import (
	"fmt"
	"strconv"
	"strings"
	"tofi-core/internal/models"
)

type Math struct{}

func (m *Math) Execute(n *models.Node, ctx *models.ExecutionContext) (string, error) {
	// 使用严格模式进行变量替换，字段不存在时会直接报错
	leftVal, err := ctx.ReplaceParamsStrict(fmt.Sprint(n.Input["left"]))
	if err != nil {
		return "", fmt.Errorf("input.left 变量替换失败: %v", err)
	}

	rightVal, err := ctx.ReplaceParamsStrict(fmt.Sprint(n.Input["right"]))
	if err != nil {
		return "", fmt.Errorf("input.right 变量替换失败: %v", err)
	}

	operator := n.Config["operator"] // ">", "<", "==", ">=", "<="

	l, errL := strconv.ParseFloat(leftVal, 64)
	r, errR := strconv.ParseFloat(rightVal, 64)
	if errL != nil || errR != nil {
		// 提供更友好的错误信息
		errMsg := "数值转换失败。"
		if errL != nil {
			errMsg += fmt.Sprintf("\n  - left = '%s' 不是有效数字", leftVal)
		}
		if errR != nil {
			errMsg += fmt.Sprintf("\n  - right = '%s' 不是有效数字", rightVal)
		}
		return "", fmt.Errorf(errMsg)
	}

	var result bool
	switch operator {
	case ">":
		result = l > r
	case "<":
		result = l < r
	case "==":
		result = l == r
	case ">=":
		result = l >= r
	case "<=":
		result = l <= r
	case "!=":
		result = l != r
	default:
		return "", fmt.Errorf("不支持的数学操作符: %s", operator)
	}

	if !result {
		if strings.ToLower(n.Config["output_bool"]) == "true" {
			return "false", nil
		}
		return fmt.Sprintf("%f %s %f 不成立", l, operator, r), fmt.Errorf("CONDITION_NOT_MET")
	}
	if strings.ToLower(n.Config["output_bool"]) == "true" {
		return "true", nil
	}
	return "MATH_PASSED", nil
}

func (m *Math) Validate(n *models.Node) error {
	if _, ok := n.Input["left"]; !ok {
		return fmt.Errorf("input.left is required")
	}
	if _, ok := n.Input["right"]; !ok {
		return fmt.Errorf("input.right is required")
	}
	op := n.Config["operator"]
	switch op {
	case ">", "<", "==", ">=", "<=", "!=":
		return nil
	default:
		return fmt.Errorf("invalid config.operator: %s", op)
	}
}

package data

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"tofi-core/internal/models"
)

type Var struct{}

// expandEnvVars 展开环境变量 ${VAR_NAME}
func expandEnvVars(s string) string {
	re := regexp.MustCompile(`\$\{([A-Z_][A-Z0-9_]*)\}`)
	return re.ReplaceAllStringFunc(s, func(match string) string {
		// 提取变量名 (去掉 ${ 和 })
		varName := match[2 : len(match)-1]
		if val := os.Getenv(varName); val != "" {
			return val
		}
		return match // 如果环境变量不存在,保留原样
	})
}

func (v *Var) Execute(n *models.Node, ctx *models.ExecutionContext) (string, error) {
	// 规范：数据存储在 Data 字段

	// 1. 单值模式优化：如果只有一个 "value" 且是字符串
	if len(n.Data) == 1 {
		if val, ok := n.Data["value"]; ok {
			if strVal, isStr := val.(string); isStr {
				// 先展开环境变量,再替换 Tofi 变量
				expanded := expandEnvVars(strVal)
				return ctx.ReplaceParams(expanded), nil
			}
		}
	}

	// 2. 字典模式：对顶层字符串值进行变量替换
	// 注意：嵌套结构（Map/List）内部的字符串暂时不支持变量替换，以保持逻辑简单
	finalData := make(map[string]interface{})
	for k, v := range n.Data {
		if strVal, ok := v.(string); ok {
			// 先展开环境变量,再替换 Tofi 变量
			expanded := expandEnvVars(strVal)
			finalData[k] = ctx.ReplaceParams(expanded)
		} else {
			finalData[k] = v // 保持原样 (int, bool, map, list)
		}
	}

	// 3. 序列化
	jsonData, err := json.Marshal(finalData)
	if err != nil {
		return "", fmt.Errorf("变量节点序列化失败: %v", err)
	}

	return string(jsonData), nil
}

func (v *Var) Validate(n *models.Node) error {
	if len(n.Data) == 0 {
		return fmt.Errorf("data field is required")
	}
	return nil
}
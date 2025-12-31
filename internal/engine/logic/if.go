package logic

import (
	"fmt"
	"strings"
	"tofi-core/internal/models"

	"github.com/Knetic/govaluate"
)

type If struct{}

func (i *If) Execute(config map[string]interface{}, ctx *models.ExecutionContext) (string, error) {
	exprStr, ok := config["if"].(string)
	if !ok {
		return "", fmt.Errorf("if expression must be a string")
	}

	functions := map[string]govaluate.ExpressionFunction{
		"contains": func(args ...interface{}) (interface{}, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("contains requires 2 arguments")
			}
			return strings.Contains(fmt.Sprint(args[0]), fmt.Sprint(args[1])), nil
		},
		"len": func(args ...interface{}) (interface{}, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("len requires 1 argument")
			}
			return float64(len(fmt.Sprint(args[0]))), nil
		},
	}

	// 注入全局结果供表达式使用 (为了方便，If 仍然可以访问全局，但推荐通过 Input 传入)
	parameters := make(map[string]interface{})
	resultsSnap, _ := ctx.Snapshot()
	for k, v := range resultsSnap {
		parameters[k] = v
	}

	expression, err := govaluate.NewEvaluableExpressionWithFunctions(exprStr, functions)
	if err != nil {
		return exprStr, fmt.Errorf("expression parse error: %v", err)
	}

	result, err := expression.Evaluate(parameters)
	if err != nil {
		return exprStr, fmt.Errorf("expression evaluation failed: %v", err)
	}

	isPassed, ok := result.(bool)
	if !ok || !isPassed {
		if strings.ToLower(fmt.Sprint(config["output_bool"])) == "true" {
			return "false", nil
		}
		return exprStr, fmt.Errorf("CONDITION_NOT_MET")
	}

	if strings.ToLower(fmt.Sprint(config["output_bool"])) == "true" {
		return "true", nil
	}
	return "EXPR_MATCHED", nil
}

func (i *If) Validate(n *models.Node) error {
	if _, ok := n.Config["if"]; !ok {
		return fmt.Errorf("config.if is required")
	}
	return nil
}

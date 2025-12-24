package engine

import (
	"fmt"
	"tofi-core/internal/executor"
	"tofi-core/internal/models"

	"github.com/Knetic/govaluate"
)

type Action interface {
	Execute(node *models.Node, ctx *models.ExecutionContext) (string, error)
}

type TaskAction struct{}

func (a *TaskAction) Execute(n *models.Node, ctx *models.ExecutionContext) (string, error) {
	return executor.ExecuteShell(ctx.ReplaceParams(n.Config["script"]), n.Timeout)
}

type LogicAction struct{}

func (a *LogicAction) Execute(n *models.Node, ctx *models.ExecutionContext) (string, error) {
	exprStr := ctx.ReplaceParams(n.Config["if"])
	expression, err := govaluate.NewEvaluableExpression(exprStr)
	if err != nil {
		return exprStr, err
	}

	result, err := expression.Evaluate(nil)
	if err != nil {
		return exprStr, err
	}

	if isPassed, ok := result.(bool); !ok || !isPassed {
		return exprStr, fmt.Errorf("CONDITION_NOT_MET")
	}
	return exprStr, nil
}

type VarAction struct{}

func (a *VarAction) Execute(n *models.Node, ctx *models.ExecutionContext) (string, error) {
	val, ok := n.Config["value"]
	if !ok {
		return "", fmt.Errorf("missing value")
	}
	return ctx.ReplaceParams(val), nil
}

type VirtualAction struct{}

func (a *VirtualAction) Execute(n *models.Node, ctx *models.ExecutionContext) (string, error) {
	return "VIRTUAL_OK", nil
}

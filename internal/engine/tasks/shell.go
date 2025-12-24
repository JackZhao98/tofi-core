package tasks

import (
	"tofi-core/internal/executor"
	"tofi-core/internal/models"
)

type Shell struct{}

func (s *Shell) Execute(n *models.Node, ctx *models.ExecutionContext) (string, error) {
	// 规范：script 属于输入数据，从 Input 读取
	script := ctx.ReplaceParams(n.Input["script"])

	// 处理 Env 变量替换
	finalEnv := make(map[string]string)
	for k, v := range n.Env {
		finalEnv[k] = ctx.ReplaceParams(v)
	}

	return executor.ExecuteShell(script, finalEnv, n.Timeout)
}

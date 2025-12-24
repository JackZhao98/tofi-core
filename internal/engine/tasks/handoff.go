package tasks

import (
	"encoding/json"
	"fmt"
	"tofi-core/internal/models"
	"tofi-core/internal/parser"
)

type Handoff struct{}

// workflowStarter 是用于启动子工作流的函数类型（避免循环依赖）
// 这个函数会在 engine 包中注入
var workflowStarter func(*models.Workflow, *models.ExecutionContext)

// SetWorkflowStarter 由 engine 包调用，注入 Start 函数
func SetWorkflowStarter(starter func(*models.Workflow, *models.ExecutionContext)) {
	workflowStarter = starter
}

func (h *Handoff) Execute(n *models.Node, ctx *models.ExecutionContext) (string, error) {
	// 1. 获取子工作流文件路径 (Config)
	filePath := ctx.ReplaceParams(n.Config["file"])
	if filePath == "" {
		return "", fmt.Errorf("missing 'file' in config")
	}

	// 2. 加载子工作流定义
	childWf, err := parser.LoadWorkflow(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to load workflow %s: %v", filePath, err)
	}

	// 3. 创建子上下文 (Child Context)
	childCtx := models.NewExecutionContext(fmt.Sprintf("%s/%s", ctx.ExecutionID, n.ID), ctx.Paths.Home)

	// 4. 注入参数 (Input -> Inputs Node Result)
	// 将 n.Input 中所有 KV 作为子工作流的初始输入
	// 我们模拟一个名为 "inputs" 的虚拟节点结果，供子工作流引用 {{inputs.xxx}}
	inputsMap := make(map[string]interface{})
	for k, v := range n.Input {
		inputsMap[k] = ctx.ReplaceParamsAny(v)
	}
	
	inputsJSON, _ := json.Marshal(inputsMap)
	childCtx.SetResult("inputs", string(inputsJSON))

	// 5. 执行子工作流 (通过注入的 starter 函数)
	if workflowStarter == nil {
		return "", fmt.Errorf("workflowStarter not initialized - call SetWorkflowStarter first")
	}
	workflowStarter(childWf, childCtx)
	
	// 6. 等待完成
	childCtx.Wg.Wait()

	// 7. 收集结果
	// 智能处理：如果 Result 是 JSON 字符串，尝试解析它，避免双重序列化
	finalOutputs := make(map[string]interface{})
	for k, v := range childCtx.Results {
		var jsonObj interface{}
		// 尝试作为 JSON 解析
		if err := json.Unmarshal([]byte(v), &jsonObj); err == nil {
			// 如果解析成功（且是对象或数组），使用解析后的对象
			finalOutputs[k] = jsonObj
		} else {
			// 否则保留原始字符串
			finalOutputs[k] = v
		}
	}

	outputsJSON, err := json.Marshal(finalOutputs)
	if err != nil {
		return "", fmt.Errorf("failed to marshal child results: %v", err)
	}

	return string(outputsJSON), nil
}

func (h *Handoff) Validate(n *models.Node) error {
	if n.Config["file"] == "" {
		return fmt.Errorf("config.file is required")
	}
	return nil
}

package tasks

import (
	"encoding/json"
	"fmt"
	"strings"
	"tofi-core/internal/models"
	"tofi-core/internal/parser"
	"tofi-core/internal/toolbox"
)

type Handoff struct{}

var workflowStarter func(*models.Workflow, *models.ExecutionContext)

func SetWorkflowStarter(starter func(*models.Workflow, *models.ExecutionContext)) {
	workflowStarter = starter
}

func (h *Handoff) Execute(config map[string]interface{}, ctx *models.ExecutionContext) (string, error) {
	var childWf *models.Workflow
	var err error

	// 1. 检查递归深度 (防止死循环)
	const MaxDepth = 10
	if ctx.Depth >= MaxDepth {
		return "", fmt.Errorf("exceeded maximum workflow recursion depth (%d)", MaxDepth)
	}

	// 2. 安全提取参数
	workflowID, _ := config["workflow"].(string)
	actionName, _ := config["action"].(string) // 兼容旧逻辑
	filePath, _ := config["file"].(string)     // 兼容旧逻辑

	// 3. 智能解析 (优先使用新的 workflow ID 模式)
	if workflowID != "" {
		// 假设 workflows 目录就在 Home 的同级 (通常是项目根目录)
		// 如果在 Server 模式下，这个目录可以以后通过配置动态注入
		workflowsRoot := "workflows"
		childWf, err = parser.ResolveWorkflow(workflowID, workflowsRoot)
		if err != nil {
			return "", fmt.Errorf("failed to resolve workflow '%s': %v", workflowID, err)
		}
	} else if actionName != "" {
		// 兼容旧的 action 逻辑
		if strings.HasPrefix(actionName, "tofi/") {
			name := strings.TrimPrefix(actionName, "tofi/")
			data, err := toolbox.ReadAction(name)
			if err != nil {
				return "", err
			}
			childWf, err = parser.ParseWorkflowFromBytes(data, "yaml")
			if err != nil {
				return "", err
			}
		} else {
			return "", fmt.Errorf("unsupported action type: %s (only tofi/... is supported)", actionName)
		}
	} else if filePath != "" {
		// 兼容旧的 file 逻辑
		childWf, err = parser.LoadWorkflow(filePath)
		if err != nil {
			return "", err
		}
	} else {
		return "", fmt.Errorf("missing 'workflow' ID in handoff task")
	}

	// 4. 创建隔离的子上下文
	childCtx := models.NewExecutionContext(ctx.ExecutionID+"/handoff", ctx.Paths.Home)
	childCtx.Depth = ctx.Depth + 1 // 递增深度
	childCtx.WorkflowName = childWf.Name

	// 继承脱敏词
	for _, s := range ctx.SecretValues {
		childCtx.AddSecretValue(s)
	}

	// 传递输入
	inputsJSON, _ := json.Marshal(config)
	childCtx.SetResult("inputs", string(inputsJSON))

	// 5. 启动子工作流
	if workflowStarter == nil {
		return "", fmt.Errorf("workflowStarter not initialized")
	}
	workflowStarter(childWf, childCtx)
	childCtx.Wg.Wait() // 同步等待子工作流结束

	// 6. 收集并返回结果
	finalOutputs := make(map[string]interface{})
	for k, v := range childCtx.Results {
		var jsonObj interface{}
		if err := json.Unmarshal([]byte(v), &jsonObj); err == nil {
			finalOutputs[k] = jsonObj
		} else {
			finalOutputs[k] = v
		}
	}

	res, _ := json.Marshal(finalOutputs)
	return string(res), nil
}

func (h *Handoff) Validate(n *models.Node) error {
	return nil
}

package tasks

import (
	"encoding/json"
	"fmt"
	"strings"
	actionlib "tofi-core/action_library"
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
	var childWf *models.Workflow
	var err error

	// 1. 优先处理 action 字段 (官方库)
	action, err := ctx.ReplaceParamsStrict(n.Config["action"])
	if err != nil {
		return "", fmt.Errorf("config.action 变量替换失败: %v", err)
	}

	if action != "" {
		if strings.HasPrefix(action, "tofi/") {
			// 提取 action 名称
			actionName := strings.TrimPrefix(action, "tofi/")

			// 从 action_library 读取
			data, err := actionlib.ReadAction(actionName)
			if err != nil {
				return "", err
			}

			// 解析 YAML
			childWf, err = parser.ParseWorkflowFromBytes(data, "yaml")
			if err != nil {
				return "", fmt.Errorf("解析官方 action %s 失败: %v", action, err)
			}
		} else {
			return "", fmt.Errorf("action 必须以 'tofi/' 开头,例如 'tofi/ai_summarize'")
		}
	} else {
		// 2. 否则使用 file 字段 (用户自定义)
		filePath, err := ctx.ReplaceParamsStrict(n.Config["file"])
		if err != nil {
			return "", fmt.Errorf("config.file 变量替换失败: %v", err)
		}

		if filePath == "" {
			return "", fmt.Errorf("必须指定 config.action 或 config.file")
		}
		// 安全检查: 禁止直接访问 action_library 目录
		if strings.Contains(filePath, "action_library/") {
			return "", fmt.Errorf("禁止直接访问 action_library 目录,请使用 action: \"tofi/xxx\" 代替")
		}

		// 加载用户工作流
		childWf, err = parser.LoadWorkflow(filePath)
		if err != nil {
			return "", fmt.Errorf("加载工作流 %s 失败: %v", filePath, err)
		}
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
	action := n.Config["action"]
	file := n.Config["file"]

	// 至少需要一个
	if action == "" && file == "" {
		return fmt.Errorf("必须指定 config.action 或 config.file")
	}

	// 如果指定了 action,验证格式和存在性
	if action != "" {
		if !strings.HasPrefix(action, "tofi/") {
			return fmt.Errorf("config.action 必须以 'tofi/' 开头,例如 'tofi/ai_summarize'")
		}

		// 验证 action 是否存在
		actionName := strings.TrimPrefix(action, "tofi/")
		if !actionlib.Exists(actionName) {
			return fmt.Errorf("官方 action 不存在: %s", action)
		}
	}

	// 如果指定了 file,检查是否试图访问 action_library
	if file != "" && strings.Contains(file, "action_library/") {
		return fmt.Errorf("禁止直接访问 action_library 目录,请使用 action: \"tofi/xxx\" 代替")
	}

	return nil
}

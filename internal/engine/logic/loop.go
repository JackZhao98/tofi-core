package logic

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"tofi-core/internal/models"
)

type Loop struct{}

func (l *Loop) Execute(n *models.Node, ctx *models.ExecutionContext) (string, error) {
	mode := n.Config["mode"]
	iterator := n.Config["iterator"]
	if iterator == "" {
		iterator = "item" // 默认循环变量名
	}

	// 1. 生成迭代项列表
	var items []interface{}
	var err error

	switch mode {
	case "list":
		items, err = l.parseListItems(n, ctx)
	case "range":
		items, err = l.generateRangeItems(n, ctx)
	default:
		return "", fmt.Errorf("不支持的 loop mode: %s (仅支持 'list' 或 'range')", mode)
	}

	if err != nil {
		return "", err
	}

	// 2. 解析子任务模板
	taskTemplate, ok := n.Input["task"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("input.task 必须是一个对象")
	}

	// 3. 并发控制参数
	maxConcurrency := 1 // 默认串行
	if concStr := n.Config["max_concurrency"]; concStr != "" {
		maxConcurrency, _ = strconv.Atoi(concStr)
		if maxConcurrency <= 0 {
			maxConcurrency = len(items) // 0 表示无限制并发
		}
	}

	// 4. 错误处理策略
	failFast := n.Config["fail_fast"] == "true" // 默认 false (继续执行)

	// 5. 批量执行
	results, err := l.executeLoop(items, iterator, taskTemplate, ctx, maxConcurrency, failFast)
	if err != nil {
		return "", err
	}

	// 6. 返回结果数组
	resultJSON, err := json.Marshal(results)
	if err != nil {
		return "", fmt.Errorf("无法序列化循环结果: %v", err)
	}

	return string(resultJSON), nil
}

// parseListItems 解析列表模式的迭代项
func (l *Loop) parseListItems(n *models.Node, ctx *models.ExecutionContext) ([]interface{}, error) {
	rawList := n.Input["items"]
	var items []interface{}

	// Case 1: 数组对象 (YAML array)
	if listObj, ok := rawList.([]interface{}); ok {
		// 递归替换变量
		replaced := ctx.ReplaceParamsAny(listObj)
		items = replaced.([]interface{})
	} else if listStr, ok := rawList.(string); ok {
		// Case 2: JSON 字符串 - 使用严格模式进行变量替换
		replacedStr, err := ctx.ReplaceParamsStrict(listStr)
		if err != nil {
			return nil, fmt.Errorf("input.items 变量替换失败: %v", err)
		}

		// 额外检查：是否为空（可能是字段值本身就是空）
		if strings.TrimSpace(replacedStr) == "" {
			return nil, fmt.Errorf("input.items 解析后为空字符串\n" +
				"  提示：引用的节点字段值为空")
		}

		if err := json.Unmarshal([]byte(replacedStr), &items); err != nil {
			// 提供更详细的错误信息
			preview := replacedStr
			if len(preview) > 100 {
				preview = preview[:100] + "..."
			}
			return nil, fmt.Errorf("列表解析失败，无法将字符串解析为 JSON 数组。\n" +
				"  接收到的内容: %s\n" +
				"  错误详情: %v\n" +
				"  提示：请确保前置节点返回的是 JSON 数组格式 [...]", preview, err)
		}
	} else {
		return nil, fmt.Errorf("input.items 必须是数组或 JSON 字符串")
	}

	return items, nil
}

// generateRangeItems 生成范围模式的迭代项
func (l *Loop) generateRangeItems(n *models.Node, ctx *models.ExecutionContext) ([]interface{}, error) {
	startVal, err := ctx.ReplaceParamsStrict(fmt.Sprint(n.Input["start"]))
	if err != nil {
		return nil, fmt.Errorf("input.start 变量替换失败: %v", err)
	}

	endVal, err := ctx.ReplaceParamsStrict(fmt.Sprint(n.Input["end"]))
	if err != nil {
		return nil, fmt.Errorf("input.end 变量替换失败: %v", err)
	}

	start, err := strconv.Atoi(startVal)
	if err != nil {
		return nil, fmt.Errorf("input.start 必须是整数: %v", err)
	}

	end, err := strconv.Atoi(endVal)
	if err != nil {
		return nil, fmt.Errorf("input.end 必须是整数: %v", err)
	}

	step := 1 // 默认步长
	if stepRaw, ok := n.Input["step"]; ok && stepRaw != nil {
		stepVal, err := ctx.ReplaceParamsStrict(fmt.Sprint(stepRaw))
		if err != nil {
			return nil, fmt.Errorf("input.step 变量替换失败: %v", err)
		}
		step, err = strconv.Atoi(stepVal)
		if err != nil || step == 0 {
			return nil, fmt.Errorf("input.step 必须是非零整数: %v", err)
		}
	}

	// 生成范围 [start, end]（闭区间）
	var items []interface{}
	if step > 0 {
		for i := start; i <= end; i += step {
			items = append(items, i)
		}
	} else {
		for i := start; i >= end; i += step {
			items = append(items, i)
		}
	}

	return items, nil
}

// executeLoop 执行批量迭代
func (l *Loop) executeLoop(
	items []interface{},
	iterator string,
	taskTemplate map[string]interface{},
	ctx *models.ExecutionContext,
	maxConcurrency int,
	failFast bool,
) ([]interface{}, error) {
	// 初始化为空数组而非 nil，确保 json.Marshal 返回 [] 而非 null
	results := make([]interface{}, 0)
	var mu sync.Mutex
	var wg sync.WaitGroup
	var firstError error

	// 信号量控制并发
	semaphore := make(chan struct{}, maxConcurrency)

	for idx, item := range items {
		// 如果 fail_fast 模式下已经有错误，停止启动新任务
		if failFast && firstError != nil {
			break
		}

		wg.Add(1)
		semaphore <- struct{}{} // 获取令牌

		go func(i int, val interface{}) {
			defer wg.Done()
			defer func() { <-semaphore }() // 释放令牌

			// 如果 fail_fast 且已有错误，直接返回
			if failFast && firstError != nil {
				return
			}

			// 创建子上下文（每次迭代独立）
			childCtx := ctx.Clone()

			// 注入循环变量
			// 智能处理：如果是简单类型（字符串、数字），直接转换；否则 JSON 序列化
			var itemValue string
			switch v := val.(type) {
			case string:
				itemValue = v
			case int, int64, float64, bool:
				itemValue = fmt.Sprint(v)
			default:
				// 复杂对象使用 JSON
				itemJSON, _ := json.Marshal(val)
				itemValue = string(itemJSON)
			}
			childCtx.SetResult(iterator, itemValue)

			// 动态构建节点
			childNode, err := l.buildNodeFromTemplate(taskTemplate, childCtx, i)
			if err != nil {
				mu.Lock()
				if failFast && firstError == nil {
					firstError = fmt.Errorf("迭代 %d 构建节点失败: %v", i, err)
				}
				results = append(results, map[string]interface{}{
					"index": i,
					"error": err.Error(),
				})
				mu.Unlock()
				return
			}

			// 执行任务
			action := getActionForLoop(childNode.Type)
			res, err := action.Execute(childNode, childCtx)

			mu.Lock()
			if err != nil {
				if failFast && firstError == nil {
					firstError = fmt.Errorf("迭代 %d 执行失败: %v", i, err)
				}
				results = append(results, map[string]interface{}{
					"index": i,
					"error": err.Error(),
				})
			} else {
				// 尝试解析 JSON 结果
				var jsonRes interface{}
				if json.Unmarshal([]byte(res), &jsonRes) == nil {
					results = append(results, jsonRes)
				} else {
					results = append(results, res)
				}
			}
			mu.Unlock()
		}(idx, item)
	}

	wg.Wait()

	if failFast && firstError != nil {
		return nil, firstError
	}

	return results, nil
}

// buildNodeFromTemplate 从模板构建节点
func (l *Loop) buildNodeFromTemplate(
	template map[string]interface{},
	ctx *models.ExecutionContext,
	index int,
) (*models.Node, error) {
	nodeType, ok := template["type"].(string)
	if !ok {
		return nil, fmt.Errorf("task.type 必须是字符串")
	}

	node := &models.Node{
		ID:   fmt.Sprintf("loop_item_%d", index),
		Type: nodeType,
	}

	// 处理 Input
	if inputRaw, ok := template["input"]; ok {
		inputReplaced := ctx.ReplaceParamsAny(inputRaw)
		if inputMap, ok := inputReplaced.(map[string]interface{}); ok {
			node.Input = inputMap
		} else {
			return nil, fmt.Errorf("task.input 必须是对象")
		}
	} else {
		node.Input = make(map[string]interface{})
	}

	// 处理 Env
	if envRaw, ok := template["env"]; ok {
		envReplaced := ctx.ReplaceParamsAny(envRaw)
		if envMap, ok := envReplaced.(map[string]interface{}); ok {
			node.Env = convertToStringMap(envMap)
		}
	} else {
		node.Env = make(map[string]string)
	}

	// 处理 Config
	if configRaw, ok := template["config"]; ok {
		if configMap, ok := configRaw.(map[string]interface{}); ok {
			node.Config = convertToStringMap(configMap)
		}
	} else {
		node.Config = make(map[string]string)
	}

	// 处理 Timeout
	if timeoutRaw, ok := template["timeout"]; ok {
		if timeoutInt, ok := timeoutRaw.(int); ok {
			node.Timeout = timeoutInt
		}
	}

	return node, nil
}

// convertToStringMap 辅助函数：将 map[string]interface{} 转为 map[string]string
func convertToStringMap(m map[string]interface{}) map[string]string {
	result := make(map[string]string)
	for k, v := range m {
		result[k] = fmt.Sprint(v)
	}
	return result
}

// actionGetter 是用于获取 Action 的函数类型（避免循环依赖）
// 这个函数会在 engine 包中注入
var actionGetter func(string) Action

// Action 接口定义（复制自 engine 包，避免循环依赖）
type Action interface {
	Execute(node *models.Node, ctx *models.ExecutionContext) (string, error)
	Validate(node *models.Node) error
}

// SetActionGetter 由 engine 包调用，注入 GetAction 函数
func SetActionGetter(getter func(string) Action) {
	actionGetter = getter
}

// getActionForLoop 获取 Action
func getActionForLoop(nodeType string) Action {
	if actionGetter == nil {
		panic("actionGetter not initialized - call SetActionGetter first")
	}
	return actionGetter(nodeType)
}

func (l *Loop) Validate(n *models.Node) error {
	// 1. 检查 mode
	mode := n.Config["mode"]
	if mode != "list" && mode != "range" {
		return fmt.Errorf("config.mode 必须是 'list' 或 'range'")
	}

	// 2. 根据 mode 检查必需的 input 字段
	if mode == "list" {
		if _, ok := n.Input["items"]; !ok {
			return fmt.Errorf("list 模式下 input.items 是必需的")
		}
	} else if mode == "range" {
		if _, ok := n.Input["start"]; !ok {
			return fmt.Errorf("range 模式下 input.start 是必需的")
		}
		if _, ok := n.Input["end"]; !ok {
			return fmt.Errorf("range 模式下 input.end 是必需的")
		}
	}

	// 3. 检查 task 字段
	taskRaw, ok := n.Input["task"]
	if !ok {
		return fmt.Errorf("input.task 是必需的")
	}

	taskTemplate, ok := taskRaw.(map[string]interface{})
	if !ok {
		return fmt.Errorf("input.task 必须是一个对象")
	}

	taskType, ok := taskTemplate["type"].(string)
	if !ok {
		return fmt.Errorf("task.type 是必需的且必须是字符串")
	}

	// 4. 黑名单验证：禁止特定类型
	forbiddenTypes := []string{
		"loop",   // 禁止嵌套循环
		"var",    // 变量定义在循环内无意义
		"const",  // 常量定义在循环内无意义
		"secret", // 密钥定义在循环内无意义
	}

	for _, forbidden := range forbiddenTypes {
		if taskType == forbidden {
			return fmt.Errorf("loop 内部禁止使用 '%s' 类型的任务（语义不合理或会导致复杂度爆炸）", taskType)
		}
	}

	// 5. 验证 max_concurrency（如果提供）
	if concStr := n.Config["max_concurrency"]; concStr != "" {
		conc, err := strconv.Atoi(concStr)
		if err != nil {
			return fmt.Errorf("config.max_concurrency 必须是整数")
		}
		if conc < 0 {
			return fmt.Errorf("config.max_concurrency 不能是负数")
		}
	}

	return nil
}

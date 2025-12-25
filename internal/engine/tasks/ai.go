package tasks

import (
	"fmt"
	"strings"
	"tofi-core/internal/executor"
	"tofi-core/internal/models"

	"github.com/tidwall/gjson"
)

type AI struct{}

func (a *AI) Execute(n *models.Node, ctx *models.ExecutionContext) (string, error) {
	// Config: 静态配置
	endpoint := ctx.ReplaceParams(n.Config["endpoint"])
	apiKey := ctx.ReplaceParams(n.Config["api_key"])
	model := ctx.ReplaceParams(n.Config["model"])
	provider := strings.ToLower(n.Config["provider"])

	// Input: 动态输入
	system, _ := n.Input["system"].(string) // system 是可选的，默认空字符串
	prompt, ok := n.Input["prompt"].(string)
	if !ok {
		return "", fmt.Errorf("AI prompt 必须是字符串")
	}

	// 使用严格模式进行变量替换
	var err error
	if system != "" {
		system, err = ctx.ReplaceParamsStrict(system)
		if err != nil {
			return "", fmt.Errorf("input.system 变量替换失败: %v", err)
		}
	}

	prompt, err = ctx.ReplaceParamsStrict(prompt)
	if err != nil {
		return "", fmt.Errorf("input.prompt 变量替换失败: %v", err)
	}

	headers := make(map[string]string)
	var payload map[string]interface{}

	// --- 多厂商适配逻辑 ---
	switch provider {
	case "gemini":
		headers["x-goog-api-key"] = apiKey
		payload = map[string]interface{}{
			"contents": []interface{}{
				map[string]interface{}{
					"parts": []map[string]string{{"text": system + "\n" + prompt}},
				},
			},
		}
	case "claude":
		headers["x-api-key"] = apiKey
		headers["anthropic-version"] = "2023-06-01"
		payload = map[string]interface{}{
			"model":      model,
			"messages":   []map[string]string{{"role": "user", "content": prompt}},
			"system":     system,
			"max_tokens": 1024,
		}
	default: // OpenAI 兼容格式 (Ollama, DeepSeek, OpenAI)
		if apiKey != "" {
			headers["Authorization"] = "Bearer " + apiKey
		}

		// 判断是否使用新的 Responses API (GPT-5+ 模型)
		useResponsesAPI := strings.HasPrefix(model, "gpt-5") || strings.Contains(endpoint, "/v1/responses")

		if useResponsesAPI {
			// 使用新的 Responses API 格式
			input := []map[string]string{}
			if system != "" {
				input = append(input, map[string]string{"role": "system", "content": system})
			}
			input = append(input, map[string]string{"role": "user", "content": prompt})

			// 根据模型确定默认的 reasoning effort
			// gpt-5.1 支持 none/low/medium/high
			// gpt-5.1-codex-max 不支持 none, 支持 low/medium/high/xhigh
			// gpt-5.2 支持 none/minimal/low/medium/high/xhigh
			defaultEffort := "low" // 安全的默认值,所有模型都支持
			if strings.Contains(model, "gpt-5.1") && !strings.Contains(model, "codex") {
				defaultEffort = "none" // gpt-5.1 (非 codex) 可以使用 none
			}

			payload = map[string]interface{}{
				"model": model,
				"input": input,
				"reasoning": map[string]string{
					"effort": defaultEffort,
				},
			}
		} else {
			// 使用旧的 Chat Completions API 格式
			payload = map[string]interface{}{
				"model": model,
				"messages": []map[string]string{
					{"role": "system", "content": system},
					{"role": "user", "content": prompt},
				},
			}
		}
	}

	resp, err := executor.PostJSON(endpoint, headers, payload, 60)
	if err != nil {
		return "", err
	}

	// 统一结果提取
	paths := []string{
		// Responses API 格式 (GPT-5+)
		"output.#(type==\"message\").content.0.text",
		"output.1.content.0.text", // 简化路径
		// Chat Completions API 格式 (GPT-4, GPT-3.5)
		"choices.0.message.content",
		// Gemini 格式
		"candidates.0.content.parts.0.text",
		// Claude 格式
		"content.0.text",
	}
	for _, path := range paths {
		if res := gjson.Get(resp, path); res.Exists() {
			return res.String(), nil
		}
	}
	return resp, fmt.Errorf("AI 响应解析失败")
}

func (a *AI) Validate(n *models.Node) error {
	if n.Config["endpoint"] == "" {
		return fmt.Errorf("config.endpoint is required")
	}
	if n.Config["model"] == "" {
		return fmt.Errorf("config.model is required")
	}
	if _, ok := n.Input["prompt"].(string); !ok {
		return fmt.Errorf("input.prompt is required and must be a string")
	}
	
	// 可选检查 provider
	provider := strings.ToLower(n.Config["provider"])
	if provider != "" && provider != "openai" && provider != "claude" && provider != "gemini" && provider != "ollama" {
		return fmt.Errorf("invalid config.provider: %s", provider)
	}
	return nil
}

package tasks

import (
	"fmt"
	"strings"
	"tofi-core/internal/executor"
	"tofi-core/internal/models"

	"github.com/tidwall/gjson"
)

type AI struct{}

func (a *AI) Execute(config map[string]interface{}, ctx *models.ExecutionContext) (string, error) {
	endpoint := fmt.Sprint(config["endpoint"])
	apiKey := fmt.Sprint(config["api_key"])
	model := fmt.Sprint(config["model"])
	provider := strings.ToLower(fmt.Sprint(config["provider"]))

	system := fmt.Sprint(config["system"])
	prompt := fmt.Sprint(config["prompt"])

	if prompt == "" {
		return "", fmt.Errorf("AI prompt cannot be empty")
	}

	headers := make(map[string]string)
	var payload map[string]interface{}

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
	default: // OpenAI 兼容格式
		if apiKey != "" {
			headers["Authorization"] = "Bearer " + apiKey
		}

		useResponsesAPI := strings.HasPrefix(model, "gpt-5") || strings.Contains(endpoint, "/v1/responses")

		if useResponsesAPI {
			input := []map[string]string{}
			if system != "" {
				input = append(input, map[string]string{"role": "system", "content": system})
			}
			input = append(input, map[string]string{"role": "user", "content": prompt})

			payload = map[string]interface{}{
				"model": model,
				"input": input,
				"reasoning": map[string]string{
					"effort": "low",
				},
			}
		} else {
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

	paths := []string{
		"output.#(type==\"message\").content.0.text",
		"choices.0.message.content",
		"candidates.0.content.parts.0.text",
		"content.0.text",
	}
	for _, path := range paths {
		if res := gjson.Get(resp, path); res.Exists() {
			return res.String(), nil
		}
	}
	return resp, fmt.Errorf("AI response parsing failed")
}

func (a *AI) Validate(n *models.Node) error {
	if _, ok := n.Config["endpoint"]; !ok {
		return fmt.Errorf("config.endpoint is required")
	}
	if _, ok := n.Config["model"]; !ok {
		return fmt.Errorf("config.model is required")
	}
	return nil
}

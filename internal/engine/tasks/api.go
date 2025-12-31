package tasks

import (
	"encoding/json"
	"fmt"
	"strings"
	"tofi-core/internal/executor"
	"tofi-core/internal/models"
)

type API struct{}

func (a *API) Execute(config map[string]interface{}, ctx *models.ExecutionContext) (string, error) {
	method := strings.ToUpper(fmt.Sprint(config["method"]))
	if method == "" {
		method = "POST"
	}
	url := fmt.Sprint(config["url"])
	if url == "" {
		return "", fmt.Errorf("API url is required")
	}

	// Body
	var body string
	if rawBody := config["body"]; rawBody != nil {
		if s, ok := rawBody.(string); ok {
			body = s
		} else {
			jsonBytes, _ := json.Marshal(rawBody)
			body = string(jsonBytes)
		}
	}

	// Headers
	headers := make(map[string]string)
	if rawHeaders := config["headers"]; rawHeaders != nil {
		if m, ok := rawHeaders.(map[string]interface{}); ok {
			for k, v := range m {
				headers[k] = fmt.Sprint(v)
			}
		}
	}

	// Params
	params := make(map[string]string)
	if rawParams := config["params"]; rawParams != nil {
		if m, ok := rawParams.(map[string]interface{}); ok {
			for k, v := range m {
				params[k] = fmt.Sprint(v)
			}
		}
	}

	resp, err := executor.ExecuteHTTP(method, url, headers, params, body, 60)
	if err != nil {
		return "", err
	}

	return resp, nil
}

func (a *API) Validate(n *models.Node) error {
	if _, ok := n.Config["url"]; !ok {
		return fmt.Errorf("config.url is required")
	}
	return nil
}

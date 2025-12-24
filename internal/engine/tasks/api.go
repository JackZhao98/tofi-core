package tasks

import (
	"encoding/json"
	"fmt"
	"strings"
	"tofi-core/internal/executor"
	"tofi-core/internal/models"
)

type API struct{}

func (a *API) Execute(n *models.Node, ctx *models.ExecutionContext) (string, error) {
	// 1. 解析 Config
	method := strings.ToUpper(ctx.ReplaceParams(n.Config["method"]))
	if method == "" {
		method = "POST"
	}
	url := ctx.ReplaceParams(n.Config["url"])

	// 2. 解析 Input: Body (支持 String 或 Object)
	var body string
	if rawBody, ok := n.Input["body"]; ok {
		if strBody, isStr := rawBody.(string); isStr {
			// 使用严格模式进行变量替换
			var err error
			body, err = ctx.ReplaceParamsStrict(strBody)
			if err != nil {
				return "", fmt.Errorf("input.body 变量替换失败: %v", err)
			}
		} else {
			// 如果是对象/列表，递归替换变量后序列化为 JSON
			processedBody := ctx.ReplaceParamsAny(rawBody)
			jsonBytes, err := json.Marshal(processedBody)
			if err != nil {
				return "", fmt.Errorf("API Body 序列化失败: %v", err)
			}
			body = string(jsonBytes)
		}
	}

	// 3. 解析 Input: Headers (支持 JSON String 或 Map)
	headers := make(map[string]string)
	
	// Legacy Config
	if apiKey := ctx.ReplaceParams(n.Config["api_key"]); apiKey != "" {
		headers["Authorization"] = "Bearer " + apiKey
	}

	if rawHeaders, ok := n.Input["headers"]; ok {
		// 情况 A: Map (YAML Object)
		if headerMap, isMap := rawHeaders.(map[string]interface{}); isMap {
			for k, v := range headerMap {
				// 使用严格模式进行变量替换
				val, err := ctx.ReplaceParamsStrict(fmt.Sprint(v))
				if err != nil {
					return "", fmt.Errorf("input.headers.%s 变量替换失败: %v", k, err)
				}
				headers[k] = val
			}
		} else if headerStr, isStr := rawHeaders.(string); isStr {
			// 情况 B: JSON String - 使用严格模式
			processedStr, err := ctx.ReplaceParamsStrict(headerStr)
			if err != nil {
				return "", fmt.Errorf("input.headers 变量替换失败: %v", err)
			}
			if processedStr != "" {
				var hm map[string]string
				if err := json.Unmarshal([]byte(processedStr), &hm); err == nil {
					for k, v := range hm {
						headers[k] = v
					}
				} else {
					return "", fmt.Errorf("invalid headers JSON: %v", err)
				}
			}
		}
	}

	// 4. 解析 Input: Params (支持 JSON String 或 Map)
	queryParams := make(map[string]string)
	if rawParams, ok := n.Input["params"]; ok {
		if paramMap, isMap := rawParams.(map[string]interface{}); isMap {
			for k, v := range paramMap {
				// 使用严格模式进行变量替换
				val, err := ctx.ReplaceParamsStrict(fmt.Sprint(v))
				if err != nil {
					return "", fmt.Errorf("input.params.%s 变量替换失败: %v", k, err)
				}
				queryParams[k] = val
			}
		} else if paramStr, isStr := rawParams.(string); isStr {
			// 使用严格模式进行变量替换
			processedStr, err := ctx.ReplaceParamsStrict(paramStr)
			if err != nil {
				return "", fmt.Errorf("input.params 变量替换失败: %v", err)
			}
			if processedStr != "" {
				var pm map[string]string
				if err := json.Unmarshal([]byte(processedStr), &pm); err == nil {
					for k, v := range pm {
						queryParams[k] = v
					}
				} else {
					return "", fmt.Errorf("invalid params JSON: %v", err)
				}
			}
		}
	}

	// 5. 执行请求
	resp, err := executor.ExecuteHTTP(method, url, headers, queryParams, body, n.Timeout)
	if err != nil {
		return "", fmt.Errorf("API 请求失败: %v", err)
	}

	return resp, nil
}

func (a *API) Validate(n *models.Node) error {
	if n.Config["url"] == "" {
		return fmt.Errorf("config.url is required")
	}

	// 检查 headers 类型
	if val, ok := n.Input["headers"]; ok {
		if _, isMap := val.(map[string]interface{}); !isMap {
			if _, isStr := val.(string); !isStr {
				return fmt.Errorf("input.headers must be a map or json string")
			}
		}
	}

	// 检查 params 类型
	if val, ok := n.Input["params"]; ok {
		if _, isMap := val.(map[string]interface{}); !isMap {
			if _, isStr := val.(string); !isStr {
				return fmt.Errorf("input.params must be a map or json string")
			}
		}
	}

	return nil
}

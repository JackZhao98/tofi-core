package executor

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ExecuteHTTP 是一个通用的 HTTP 请求执行器
func ExecuteHTTP(method, targetURL string, headers map[string]string, queryParams map[string]string, bodyStr string, timeout int) (string, error) {
	// 1. 处理超时
	if timeout <= 0 {
		timeout = 30
	}
	client := &http.Client{Timeout: time.Duration(timeout) * time.Second}

	// 2. 处理 Query Params
	if len(queryParams) > 0 {
		u, err := url.Parse(targetURL)
		if err != nil {
			return "", fmt.Errorf("无效的 URL: %v", err)
		}
		q := u.Query()
		for k, v := range queryParams {
			q.Add(k, v)
		}
		u.RawQuery = q.Encode()
		targetURL = u.String()
	}

	// 3. 构造请求体
	var bodyReader io.Reader
	if bodyStr != "" {
		bodyReader = strings.NewReader(bodyStr)
	}

	req, err := http.NewRequest(method, targetURL, bodyReader)
	if err != nil {
		return "", fmt.Errorf("创建请求失败: %v", err)
	}

	// 4. 设置 Headers
	// 默认 Content-Type，如果用户没传，且有 body，默认为 JSON
	hasContentType := false
	for k := range headers {
		if http.CanonicalHeaderKey(k) == "Content-Type" {
			hasContentType = true
			break
		}
	}
	if bodyStr != "" && !hasContentType {
		req.Header.Set("Content-Type", "application/json")
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	// 5. 执行请求
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求执行失败: %v", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("读取响应失败: %v", err)
	}

	// 6. 状态码检查 (允许 2xx)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return string(respBody), fmt.Errorf("HTTP Error %d: %s", resp.StatusCode, string(respBody))
	}

	return string(respBody), nil
}

// PostJSON 发送通用的 OpenAI 兼容请求 (保留给 AI 任务使用)
func PostJSON(url string, headers map[string]string, payload interface{}, timeout int) (string, error) {
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	// 复用 ExecuteHTTP
	// 注意：PostJSON 默认会加上 Content-Type: application/json (ExecuteHTTP 已处理)
	return ExecuteHTTP("POST", url, headers, nil, string(jsonData), timeout)
}

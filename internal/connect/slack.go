package connect

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// SendSlackWebhook 通过 Slack Incoming Webhook 发送消息
func SendSlackWebhook(webhookURL, text string) error {
	payload, _ := json.Marshal(map[string]string{"text": text})

	req, err := http.NewRequest("POST", webhookURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("slack webhook: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("slack webhook: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	// Slack webhook 成功返回 "ok" 文本（不是 JSON）
	if resp.StatusCode == 200 && string(body) == "ok" {
		return nil
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	return fmt.Errorf("slack webhook: HTTP %d: %s", resp.StatusCode, string(body))
}

// ValidateSlackWebhook 验证 Slack Webhook URL 是否有效
// 发送一条空消息测试 — Slack 会返回 "no_text" 错误但证明 URL 有效
func ValidateSlackWebhook(webhookURL string) error {
	payload, _ := json.Marshal(map[string]string{})

	req, err := http.NewRequest("POST", webhookURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("slack webhook unreachable: %w", err)
	}
	defer resp.Body.Close()

	// Slack 返回 400 + "no_text" 表示 URL 有效但消息为空
	// 返回 403/404 表示 URL 无效
	if resp.StatusCode == 400 {
		return nil // URL is valid, just no text
	}
	if resp.StatusCode == 200 {
		return nil
	}

	body, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("slack webhook returned HTTP %d: %s", resp.StatusCode, string(body))
}

package connect

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// SendDiscordWebhook 通过 Discord Webhook 发送消息
func SendDiscordWebhook(webhookURL, text string) error {
	payload, _ := json.Marshal(map[string]string{"content": text})

	req, err := http.NewRequest("POST", webhookURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("discord webhook: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("discord webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	body, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("discord webhook: HTTP %d: %s", resp.StatusCode, string(body))
}

// ValidateDiscordWebhook 验证 Discord Webhook URL 是否有效
// Discord webhook URL 格式: https://discord.com/api/webhooks/{id}/{token}
func ValidateDiscordWebhook(webhookURL string) error {
	req, err := http.NewRequest("GET", webhookURL, nil)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("discord webhook unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("discord webhook returned HTTP %d", resp.StatusCode)
	}

	var info struct {
		Name string `json:"name"`
		ID   string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return fmt.Errorf("invalid webhook response: %w", err)
	}
	if info.ID == "" {
		return fmt.Errorf("webhook returned no ID")
	}

	return nil
}

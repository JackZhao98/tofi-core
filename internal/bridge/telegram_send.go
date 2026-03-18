package bridge

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	telegramAPIBase   = "https://api.telegram.org/bot"
	telegramMaxMsgLen = 4096
	telegramTypingTTL = 4 * time.Second
)

// TelegramSender 封装 Telegram Bot API 消息发送
type TelegramSender struct {
	BotToken string
}

// SendMessage 发送消息，先尝试 Markdown，失败则 fallback 到纯文本。
// 超过 4096 字符自动分片。
func (ts *TelegramSender) SendMessage(chatID, text string) error {
	if text == "" {
		return nil
	}
	chunks := splitMessage(text, telegramMaxMsgLen)
	for _, chunk := range chunks {
		if err := ts.sendSingle(chatID, chunk); err != nil {
			return err
		}
	}
	return nil
}

// sendSingle 发送单条消息，先 Markdown 后 fallback 纯文本，带重试
func (ts *TelegramSender) sendSingle(chatID, text string) error {
	err := ts.sendWithRetry(chatID, text, "Markdown")
	if err != nil && strings.Contains(err.Error(), "400") {
		return ts.sendWithRetry(chatID, text, "")
	}
	return err
}

// sendWithRetry 带重试的发送（3 次，间隔 1/2/4 秒）
func (ts *TelegramSender) sendWithRetry(chatID, text, parseMode string) error {
	var lastErr error
	for i := 0; i < 3; i++ {
		lastErr = ts.sendRaw(chatID, text, parseMode)
		if lastErr == nil {
			return nil
		}
		if strings.Contains(lastErr.Error(), "400") {
			return lastErr
		}
		time.Sleep(time.Duration(1<<uint(i)) * time.Second)
	}
	return lastErr
}

// sendRaw 底层 Telegram sendMessage API 调用
func (ts *TelegramSender) sendRaw(chatID, text, parseMode string) error {
	params := url.Values{
		"chat_id": {chatID},
		"text":    {text},
	}
	if parseMode != "" {
		params.Set("parse_mode", parseMode)
	}
	apiURL := telegramAPIBase + ts.BotToken + "/sendMessage"
	resp, err := http.PostForm(apiURL, params)
	if err != nil {
		return fmt.Errorf("telegram sendMessage failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telegram sendMessage %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// SendTyping 发送"正在输入"状态
func (ts *TelegramSender) SendTyping(chatID string) error {
	params := url.Values{
		"chat_id": {chatID},
		"action":  {"typing"},
	}
	apiURL := telegramAPIBase + ts.BotToken + "/sendChatAction"
	resp, err := http.PostForm(apiURL, params)
	if err != nil {
		return nil
	}
	resp.Body.Close()
	return nil
}

// splitMessage 将超长文本分片，尽量在换行符处断开
func splitMessage(text string, maxLen int) []string {
	if len(text) <= maxLen {
		return []string{text}
	}
	var chunks []string
	for len(text) > 0 {
		if len(text) <= maxLen {
			chunks = append(chunks, text)
			break
		}
		cutAt := maxLen
		if idx := strings.LastIndex(text[:maxLen], "\n"); idx > maxLen/2 {
			cutAt = idx + 1
		}
		chunks = append(chunks, text[:cutAt])
		text = text[cutAt:]
	}
	return chunks
}

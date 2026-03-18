package bridge

import (
	"strings"
	"sync"
	"time"
)

const (
	defaultFlushInterval = 2 * time.Second
	defaultMaxChars      = 500
)

// StreamBuffer 汇聚 Agent 的流式输出，定时或满容量时 flush 到 Telegram。
type StreamBuffer struct {
	sender *TelegramSender
	chatID string
	buf    strings.Builder
	mu     sync.Mutex
	timer  *time.Timer
}

// NewStreamBuffer 创建新的流式缓冲
func NewStreamBuffer(sender *TelegramSender, chatID string) *StreamBuffer {
	sb := &StreamBuffer{
		sender: sender,
		chatID: chatID,
	}
	sb.timer = time.AfterFunc(defaultFlushInterval, sb.timerFlush)
	sb.timer.Stop()
	return sb
}

// Write 写入一段 delta 文本
func (sb *StreamBuffer) Write(delta string) {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	sb.buf.WriteString(delta)
	sb.timer.Reset(defaultFlushInterval)
	if sb.buf.Len() >= defaultMaxChars {
		sb.flushLocked()
	}
}

// Flush 手动 flush
func (sb *StreamBuffer) Flush() {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	sb.flushLocked()
}

// Close 关闭 buffer，flush 剩余内容
func (sb *StreamBuffer) Close() {
	sb.timer.Stop()
	sb.Flush()
}

// flushLocked 发送 buffer 内容（必须持有锁）
func (sb *StreamBuffer) flushLocked() {
	sb.timer.Stop()
	text := strings.TrimSpace(sb.buf.String())
	sb.buf.Reset()
	if text == "" {
		return
	}
	go func() {
		_ = sb.sender.SendMessage(sb.chatID, text)
	}()
}

// timerFlush 定时器触发的 flush
func (sb *StreamBuffer) timerFlush() {
	sb.Flush()
}

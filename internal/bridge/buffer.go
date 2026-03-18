package bridge

import (
	"strings"
	"sync"
	"time"
)

const (
	defaultFlushInterval = 1 * time.Second
	defaultMaxChars      = 3800 // 留余量给 4096 限制
)

// StreamBuffer 汇聚 Agent 的流式输出，通过编辑同一条 Telegram 消息模拟流式效果。
// 当累积内容超过 Telegram 消息上限时，发送新消息继续。
type StreamBuffer struct {
	sender    *TelegramSender
	chatID    string
	buf       strings.Builder // 当前消息的完整内容（不是增量）
	mu        sync.Mutex
	timer     *time.Timer
	messageID int64  // 当前正在编辑的消息 ID（0 = 还没发过）
	lastSent  string // 上次发送/编辑的内容（避免重复编辑相同内容）
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

	// 超过单条消息上限 → 立即 flush 并开始新消息
	if sb.buf.Len() >= defaultMaxChars {
		sb.flushLocked()
		// 重置 messageID，下次 flush 会发新消息
		sb.messageID = 0
		sb.lastSent = ""
	}
}

// Flush 手动 flush（工具调用开始时、Agent 完成时调用）
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

// flushLocked 发送或编辑消息（必须持有锁）
func (sb *StreamBuffer) flushLocked() {
	sb.timer.Stop()

	text := strings.TrimSpace(sb.buf.String())
	if text == "" || text == sb.lastSent {
		return // 没有新内容或内容没变
	}

	sb.lastSent = text

	if sb.messageID == 0 {
		// 首次发送 → sendMessage 并记录 messageID
		msgID, err := sb.sender.SendMessageReturnID(sb.chatID, text)
		if err == nil && msgID > 0 {
			sb.messageID = msgID
		}
	} else {
		// 后续更新 → editMessageText
		err := sb.sender.EditMessage(sb.chatID, sb.messageID, text)
		if err != nil {
			// 编辑失败（消息被删除等），发新消息
			msgID, sendErr := sb.sender.SendMessageReturnID(sb.chatID, text)
			if sendErr == nil && msgID > 0 {
				sb.messageID = msgID
			}
		}
	}
}

// FinalizeAndReset flush 当前内容并重置，为下一段内容做准备（新消息）
func (sb *StreamBuffer) FinalizeAndReset() {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	sb.flushLocked()
	sb.messageID = 0
	sb.lastSent = ""
	sb.buf.Reset()
}

// timerFlush 定时器触发的 flush
func (sb *StreamBuffer) timerFlush() {
	sb.Flush()
}

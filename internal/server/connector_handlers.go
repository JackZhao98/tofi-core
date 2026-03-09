package server

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"
	"tofi-core/internal/notify"
)

// PendingVerify 待验证的 Telegram 连接
type PendingVerify struct {
	Code     string
	BotToken string
	Done     chan *notify.VerifiedUser // 验证成功后发送用户信息
}

// pendingVerifies 管理待验证的 Telegram 连接
var (
	pendingVerifiesMu sync.Mutex
	pendingVerifies   = make(map[string]*PendingVerify) // userID → pending
)

// handleTelegramSetup POST /api/v1/connectors/telegram/setup
// 保存 bot token，验证有效性，返回 bot info
func (s *Server) handleTelegramSetup(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")

	var req struct {
		BotToken string `json:"bot_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.BotToken == "" {
		http.Error(w, `{"error":"bot_token required"}`, http.StatusBadRequest)
		return
	}

	// 验证 token 有效性并获取 bot 信息
	info, err := notify.GetBotInfo(req.BotToken)
	if err != nil {
		http.Error(w, `{"error":"invalid bot token: `+err.Error()+`"}`, http.StatusBadRequest)
		return
	}

	// 保存到 settings
	if err := s.db.SetTelegramBotToken(userID, req.BotToken, info.Name, info.Username, info.PhotoURL); err != nil {
		http.Error(w, `{"error":"failed to save"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"bot_name":     info.Name,
		"bot_username": info.Username,
		"bot_photo":    info.PhotoURL,
	})
}

// handleTelegramStatus GET /api/v1/connectors/telegram/status
func (s *Server) handleTelegramStatus(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")

	cfg, err := s.db.GetTelegramConfig(userID)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"configured": false,
			"enabled":    false,
			"receivers":  []any{},
		})
		return
	}

	receivers, _ := s.db.ListTelegramReceivers(userID)

	// 检查是否有 pending 验证
	pendingVerifiesMu.Lock()
	_, verifying := pendingVerifies[userID]
	pendingVerifiesMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"configured":   true,
		"enabled":      cfg.Enabled,
		"bot_name":     cfg.BotName,
		"bot_username": cfg.BotUsername,
		"bot_photo":    cfg.BotPhoto,
		"verifying":    verifying,
		"receivers":    receivers,
	})
}

// handleTelegramVerify POST /api/v1/connectors/telegram/verify
// 生成验证码，开始 long polling 等待用户发送验证码
func (s *Server) handleTelegramVerify(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")

	cfg, err := s.db.GetTelegramConfig(userID)
	if err != nil || cfg.BotToken == "" {
		http.Error(w, `{"error":"telegram not configured, setup first"}`, http.StatusBadRequest)
		return
	}

	// 取消之前的 pending 验证
	pendingVerifiesMu.Lock()
	if old, ok := pendingVerifies[userID]; ok {
		close(old.Done)
		delete(pendingVerifies, userID)
	}
	pendingVerifiesMu.Unlock()

	done := make(chan *notify.VerifiedUser, 1)

	// 生成 4 位验证码，确保同一 bot token 下唯一
	pendingVerifiesMu.Lock()
	var code string
	for {
		code = notify.GenerateVerifyCode()
		unique := true
		for uid, p := range pendingVerifies {
			if uid != userID && p.BotToken == cfg.BotToken && p.Code == code {
				unique = false
				break
			}
		}
		if unique {
			break
		}
	}
	pendingVerifies[userID] = &PendingVerify{
		Code:     code,
		BotToken: cfg.BotToken,
		Done:     done,
	}
	pendingVerifiesMu.Unlock()

	// 后台 goroutine polling
	go func() {
		defer func() {
			pendingVerifiesMu.Lock()
			delete(pendingVerifies, userID)
			pendingVerifiesMu.Unlock()
		}()

		verified, err := notify.PollForVerifyCode(cfg.BotToken, code, 5*time.Minute)
		if err != nil {
			log.Printf("[telegram] verify polling failed for user %s: %v", userID, err)
			return
		}

		// 保存 receiver
		_, err = s.db.AddTelegramReceiver(userID, verified.ChatID, verified.DisplayName, verified.Username, verified.AvatarURL)
		if err != nil {
			log.Printf("[telegram] failed to save receiver for user %s: %v", userID, err)
			return
		}

		log.Printf("[telegram] user %s verified receiver: %s (@%s)", userID, verified.DisplayName, verified.Username)

		select {
		case done <- verified:
		default:
		}
	}()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"code":         code,
		"bot_name":     cfg.BotName,
		"bot_username": cfg.BotUsername,
	})
}

// handleTelegramTest POST /api/v1/connectors/telegram/test
// 可选 receiver_id：指定则只发给该用户，否则发给所有 receiver
func (s *Server) handleTelegramTest(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")

	cfg, err := s.db.GetTelegramConfig(userID)
	if err != nil || cfg.BotToken == "" {
		http.Error(w, `{"error":"telegram not configured"}`, http.StatusBadRequest)
		return
	}

	var req struct {
		ReceiverID *int64 `json:"receiver_id"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	msg := "✅ *Tofi Connected!*\n\nYou'll receive task notifications here."

	if req.ReceiverID != nil {
		receiver, err := s.db.GetTelegramReceiver(userID, *req.ReceiverID)
		if err != nil {
			http.Error(w, `{"error":"receiver not found"}`, http.StatusNotFound)
			return
		}
		if err := notify.SendMessage(cfg.BotToken, receiver.ChatID, msg); err != nil {
			log.Printf("[telegram] test message failed for %s: %v", receiver.ChatID, err)
		}
	} else {
		receivers, _ := s.db.ListTelegramReceivers(userID)
		if len(receivers) == 0 {
			http.Error(w, `{"error":"no receivers connected"}`, http.StatusBadRequest)
			return
		}
		for _, rv := range receivers {
			if err := notify.SendMessage(cfg.BotToken, rv.ChatID, msg); err != nil {
				log.Printf("[telegram] test message failed for %s: %v", rv.ChatID, err)
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

// handleTelegramDeleteReceiver DELETE /api/v1/connectors/telegram/receivers/{id}
func (s *Server) handleTelegramDeleteReceiver(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
		return
	}

	// 删除前查出 receiver 信息，发 Telegram 通知
	receiver, _ := s.db.GetTelegramReceiver(userID, id)
	if receiver != nil {
		cfg, _ := s.db.GetTelegramConfig(userID)
		if cfg != nil && cfg.BotToken != "" {
			go notify.SendMessage(cfg.BotToken, receiver.ChatID, "🔕 You have been removed from Tofi notifications.")
		}
	}

	if err := s.db.DeleteTelegramReceiver(userID, id); err != nil {
		http.Error(w, `{"error":"failed to delete"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

// handleTelegramDelete DELETE /api/v1/connectors/telegram
// 删除整个 Telegram 配置 + 所有 receiver
func (s *Server) handleTelegramDelete(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")

	// 取消 pending 验证
	pendingVerifiesMu.Lock()
	if old, ok := pendingVerifies[userID]; ok {
		close(old.Done)
		delete(pendingVerifies, userID)
	}
	pendingVerifiesMu.Unlock()

	if err := s.db.DeleteTelegramConfig(userID); err != nil {
		http.Error(w, `{"error":"failed to delete"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

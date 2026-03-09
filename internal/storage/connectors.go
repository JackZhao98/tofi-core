package storage

import (
	"fmt"
	"time"
)

// TelegramConfig Telegram Bot 连接配置（settings 表）
type TelegramConfig struct {
	BotToken    string `json:"bot_token"`
	Enabled     bool   `json:"enabled"`
	BotName     string `json:"bot_name"`
	BotUsername string `json:"bot_username"`
	BotPhoto    string `json:"bot_photo"`
}

// TelegramReceiver 一个 Telegram 通知接收者（独立表）
type TelegramReceiver struct {
	ID          int64  `json:"id"`
	UserID      string `json:"user_id"`
	ChatID      string `json:"chat_id"`
	DisplayName string `json:"display_name"`
	Username    string `json:"username"`
	AvatarURL   string `json:"avatar_url"`
	ConnectedAt string `json:"connected_at"`
}

// --- Settings 表 key 约定 ---
const (
	keyTelegramBotToken    = "connector_telegram_bot_token"
	keyTelegramEnabled     = "connector_telegram_enabled"
	keyTelegramBotName     = "connector_telegram_bot_name"
	keyTelegramBotUsername = "connector_telegram_bot_username"
	keyTelegramBotPhoto    = "connector_telegram_bot_photo"
)

// initTelegramReceiversTable 创建 telegram_receivers 表
func (db *DB) initTelegramReceiversTable() error {
	query := `
	CREATE TABLE IF NOT EXISTS telegram_receivers (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id TEXT NOT NULL,
		chat_id TEXT NOT NULL,
		display_name TEXT NOT NULL DEFAULT '',
		username TEXT NOT NULL DEFAULT '',
		avatar_url TEXT NOT NULL DEFAULT '',
		connected_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(user_id, chat_id)
	);`
	_, err := db.conn.Exec(query)
	return err
}

// --- Bot Config (settings 表) ---

// GetTelegramConfig 获取用户的 Telegram Bot 配置
func (db *DB) GetTelegramConfig(userID string) (*TelegramConfig, error) {
	cfg := &TelegramConfig{}

	rows, err := db.conn.Query(
		`SELECT key, value FROM settings WHERE scope = ? AND key LIKE 'connector_telegram_%'`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	found := false
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			continue
		}
		found = true
		switch key {
		case keyTelegramBotToken:
			cfg.BotToken = value
		case keyTelegramEnabled:
			cfg.Enabled = value == "true"
		case keyTelegramBotName:
			cfg.BotName = value
		case keyTelegramBotUsername:
			cfg.BotUsername = value
		case keyTelegramBotPhoto:
			cfg.BotPhoto = value
		}
	}

	if !found {
		return nil, fmt.Errorf("no telegram config for user %s", userID)
	}
	return cfg, nil
}

// SetTelegramBotToken 保存 bot token 及 bot 信息
func (db *DB) SetTelegramBotToken(userID, token, botName, botUsername, botPhoto string) error {
	if err := db.SetSetting(keyTelegramBotToken, userID, token); err != nil {
		return err
	}
	if err := db.SetSetting(keyTelegramBotName, userID, botName); err != nil {
		return err
	}
	if err := db.SetSetting(keyTelegramBotUsername, userID, botUsername); err != nil {
		return err
	}
	if err := db.SetSetting(keyTelegramBotPhoto, userID, botPhoto); err != nil {
		return err
	}
	return db.SetSetting(keyTelegramEnabled, userID, "true")
}

// SetTelegramEnabled 启用/禁用 Telegram 通知
func (db *DB) SetTelegramEnabled(userID string, enabled bool) error {
	val := "false"
	if enabled {
		val = "true"
	}
	return db.SetSetting(keyTelegramEnabled, userID, val)
}

// DeleteTelegramConfig 删除用户所有 Telegram 配置 + receivers
func (db *DB) DeleteTelegramConfig(userID string) error {
	_, _ = db.conn.Exec(`DELETE FROM telegram_receivers WHERE user_id = ?`, userID)
	_, err := db.conn.Exec(
		`DELETE FROM settings WHERE scope = ? AND key LIKE 'connector_telegram_%'`,
		userID,
	)
	return err
}

// --- Receiver 管理（独立表） ---

// AddTelegramReceiver 添加一个接收者（去重 by chat_id）
func (db *DB) AddTelegramReceiver(userID, chatID, displayName, username, avatarURL string) (*TelegramReceiver, error) {
	now := time.Now().Format("2006-01-02 15:04:05")
	result, err := db.conn.Exec(
		`INSERT INTO telegram_receivers (user_id, chat_id, display_name, username, avatar_url, connected_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(user_id, chat_id) DO UPDATE SET
		   display_name = excluded.display_name,
		   username = excluded.username,
		   avatar_url = excluded.avatar_url,
		   connected_at = excluded.connected_at`,
		userID, chatID, displayName, username, avatarURL, now,
	)
	if err != nil {
		return nil, err
	}
	id, _ := result.LastInsertId()
	return &TelegramReceiver{
		ID:          id,
		UserID:      userID,
		ChatID:      chatID,
		DisplayName: displayName,
		Username:    username,
		AvatarURL:   avatarURL,
		ConnectedAt: now,
	}, nil
}

// ListTelegramReceivers 列出用户的所有接收者
func (db *DB) ListTelegramReceivers(userID string) ([]*TelegramReceiver, error) {
	rows, err := db.conn.Query(
		`SELECT id, user_id, chat_id, display_name, username, avatar_url, connected_at
		 FROM telegram_receivers WHERE user_id = ? ORDER BY connected_at`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var receivers []*TelegramReceiver
	for rows.Next() {
		r := &TelegramReceiver{}
		if err := rows.Scan(&r.ID, &r.UserID, &r.ChatID, &r.DisplayName, &r.Username, &r.AvatarURL, &r.ConnectedAt); err != nil {
			continue
		}
		receivers = append(receivers, r)
	}
	return receivers, nil
}

// GetTelegramReceiver 获取单个接收者
func (db *DB) GetTelegramReceiver(userID string, receiverID int64) (*TelegramReceiver, error) {
	r := &TelegramReceiver{}
	err := db.conn.QueryRow(
		`SELECT id, user_id, chat_id, display_name, username, avatar_url, connected_at
		 FROM telegram_receivers WHERE id = ? AND user_id = ?`,
		receiverID, userID,
	).Scan(&r.ID, &r.UserID, &r.ChatID, &r.DisplayName, &r.Username, &r.AvatarURL, &r.ConnectedAt)
	if err != nil {
		return nil, err
	}
	return r, nil
}

// DeleteTelegramReceiver 删除一个接收者
func (db *DB) DeleteTelegramReceiver(userID string, receiverID int64) error {
	_, err := db.conn.Exec(
		`DELETE FROM telegram_receivers WHERE id = ? AND user_id = ?`,
		receiverID, userID,
	)
	return err
}

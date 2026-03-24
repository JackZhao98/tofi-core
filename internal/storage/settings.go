package storage

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	"tofi-core/internal/crypto"

	"github.com/google/uuid"
)

// SettingRecord 系统/用户级设置
type SettingRecord struct {
	Key       string `json:"key"`
	Value     string `json:"value"`
	Scope     string `json:"scope"`      // "system" | 用户ID
	UpdatedAt string `json:"updated_at"`
}

// initSettingsTable 创建 settings 表
func (db *DB) initSettingsTable() error {
	query := `
	CREATE TABLE IF NOT EXISTS settings (
		key TEXT NOT NULL,
		scope TEXT NOT NULL DEFAULT 'system',
		value TEXT NOT NULL,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (key, scope)
	);`

	_, err := db.conn.Exec(query)
	return err
}

// --- AI Key Management ---

// GetSetting 获取设置值（先查用户级，再查系统级）
func (db *DB) GetSetting(key, userID string) (string, error) {
	// 优先用户级
	if userID != "" {
		var val string
		err := db.conn.QueryRow("SELECT value FROM settings WHERE key = ? AND scope = ?", key, userID).Scan(&val)
		if err == nil {
			return val, nil
		}
	}
	// 回退系统级
	var val string
	err := db.conn.QueryRow("SELECT value FROM settings WHERE key = ? AND scope = 'system'", key).Scan(&val)
	if err != nil {
		return "", err
	}
	return val, nil
}

// SetSetting 设置值
func (db *DB) SetSetting(key, scope, value string) error {
	query := `
	INSERT INTO settings (key, scope, value, updated_at) VALUES (?, ?, ?, ?)
	ON CONFLICT(key, scope) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`

	now := time.Now().Format("2006-01-02 15:04:05")
	_, err := db.conn.Exec(query, key, scope, value, now)
	return err
}

// DeleteSetting 删除设置
func (db *DB) DeleteSetting(key, scope string) error {
	_, err := db.conn.Exec("DELETE FROM settings WHERE key = ? AND scope = ?", key, scope)
	return err
}

// ListSettings 列出某 scope 的所有设置
func (db *DB) ListSettings(scope string) ([]*SettingRecord, error) {
	query := `SELECT key, scope, value, updated_at FROM settings WHERE scope = ? ORDER BY key`
	rows, err := db.conn.Query(query, scope)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []*SettingRecord
	for rows.Next() {
		var r SettingRecord
		if err := rows.Scan(&r.Key, &r.Scope, &r.Value, &r.UpdatedAt); err != nil {
			continue
		}
		records = append(records, &r)
	}
	return records, nil
}

// --- 便捷方法: AI API Key (加密存储在 secrets 表) ---

// AI Key 存储约定:
//   secrets 表: user = scope ("system" 或 userID), name = "ai_key_{provider}"
//   encrypted_value = AES-256-GCM 加密后的 API Key

// GetAIKey 获取指定 scope 的 AI API Key（解密返回明文）
func (db *DB) GetAIKey(provider, userID string) (string, error) {
	name := "ai_key_" + provider

	// 优先用户级
	if userID != "" {
		secret, err := db.GetSecret(userID, name)
		if err == nil {
			plaintext, err := crypto.Decrypt(secret.EncryptedValue)
			if err != nil {
				return "", fmt.Errorf("failed to decrypt AI key: %w", err)
			}
			return plaintext, nil
		}
	}
	// 回退系统级
	secret, err := db.GetSecret("system", name)
	if err != nil {
		return "", sql.ErrNoRows
	}
	plaintext, err := crypto.Decrypt(secret.EncryptedValue)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt AI key: %w", err)
	}
	return plaintext, nil
}

// SetAIKey 设置 AI API Key（加密后存入 secrets 表）
func (db *DB) SetAIKey(provider, scope, apiKey string) error {
	name := "ai_key_" + provider
	encrypted, err := crypto.Encrypt(apiKey)
	if err != nil {
		return fmt.Errorf("failed to encrypt AI key: %w", err)
	}
	id := uuid.New().String()
	return db.SaveSecret(id, scope, name, encrypted)
}

// DeleteAIKey 删除 AI API Key
func (db *DB) DeleteAIKey(provider, scope string) error {
	name := "ai_key_" + provider
	return db.DeleteSecret(scope, name)
}

// ListAIKeys 列出所有 AI Key 配置（不返回完整 key，仅掩码）
func (db *DB) ListAIKeys(scope string) ([]map[string]string, error) {
	query := `SELECT name, encrypted_value, updated_at FROM secrets WHERE user = ? AND name LIKE 'ai_key_%' ORDER BY name`
	rows, err := db.conn.Query(query, scope)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []map[string]string
	for rows.Next() {
		var name, encValue string
		var updatedAt sql.NullString
		if err := rows.Scan(&name, &encValue, &updatedAt); err != nil {
			continue
		}
		// 提取 provider 名称: "ai_key_anthropic" → "anthropic"
		provider := name[7:]
		// 解密后掩码
		plaintext, err := crypto.Decrypt(encValue)
		masked := "****"
		if err == nil {
			masked = maskAPIKey(plaintext)
		}
		ts := ""
		if updatedAt.Valid {
			ts = updatedAt.String
		}
		results = append(results, map[string]string{
			"provider":   provider,
			"masked_key": masked,
			"updated_at": ts,
		})
	}
	return results, nil
}

// maskAPIKey 掩码 API Key: "sk-abc...xyz" → "sk-a****xyz"
func maskAPIKey(key string) string {
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + "****" + key[len(key)-4:]
}

// ResolveAIKey 解析 AI Key（优先级：用户 > 系统 > 报错）
func (db *DB) ResolveAIKey(provider, userID string) (string, error) {
	name := "ai_key_" + provider
	// 用户级
	if userID != "" {
		secret, err := db.GetSecret(userID, name)
		if err == nil {
			plaintext, err := crypto.Decrypt(secret.EncryptedValue)
			if err == nil && plaintext != "" {
				return plaintext, nil
			}
		}
	}
	// 系统级
	secret, err := db.GetSecret("system", name)
	if err == nil {
		plaintext, err := crypto.Decrypt(secret.EncryptedValue)
		if err == nil && plaintext != "" {
			return plaintext, nil
		}
	}
	return "", sql.ErrNoRows
}

// --- Admin 开关 ---

// AllowUserKeys 返回是否允许用户自带 API Key（默认 true）
func (db *DB) AllowUserKeys() bool {
	val, err := db.GetSetting("allow_user_keys", "")
	if err != nil || val == "" {
		return true // 默认允许
	}
	return val == "true"
}

// SetAllowUserKeys 设置是否允许用户自带 API Key
func (db *DB) SetAllowUserKeys(allow bool) error {
	val := "false"
	if allow {
		val = "true"
	}
	return db.SetSetting("allow_user_keys", "system", val)
}

// --- 数据迁移: settings 明文 → secrets 加密 ---

// MigrateAIKeysToSecrets 将 settings 表中的明文 AI Key 迁移到 secrets 表（加密存储）
func (db *DB) MigrateAIKeysToSecrets() error {
	query := `SELECT key, scope, value FROM settings WHERE key LIKE 'ai_key_%'`
	rows, err := db.conn.Query(query)
	if err != nil {
		return nil // 表可能不存在，忽略
	}
	defer rows.Close()

	var migrations []struct{ key, scope, value string }
	for rows.Next() {
		var k, s, v string
		if err := rows.Scan(&k, &s, &v); err != nil {
			continue
		}
		migrations = append(migrations, struct{ key, scope, value string }{k, s, v})
	}

	if len(migrations) == 0 {
		return nil
	}

	log.Printf("🔐 Migrating %d AI key(s) from plaintext to encrypted storage...", len(migrations))

	for _, m := range migrations {
		provider := m.key[7:] // "ai_key_anthropic" → "anthropic"
		if err := db.SetAIKey(provider, m.scope, m.value); err != nil {
			log.Printf("⚠️  Failed to migrate AI key %s (scope=%s): %v", m.key, m.scope, err)
			continue
		}
		// 删除 settings 表中的明文记录
		if err := db.DeleteSetting(m.key, m.scope); err != nil {
			log.Printf("⚠️  Failed to delete plaintext AI key %s: %v", m.key, err)
		}
	}

	log.Printf("✅ AI key migration complete")
	return nil
}

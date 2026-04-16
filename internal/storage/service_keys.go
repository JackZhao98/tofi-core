package storage

import (
	"database/sql"
	"fmt"
	"time"

	"tofi-core/internal/crypto"

	"github.com/google/uuid"
)

// Service keys are 3rd-party API credentials that non-LLM skills need
// (Brave search, Tavily, etc.). They live in the same `secrets` table as
// AI provider keys but use a distinct `service_key_` prefix so the two
// conceptual groups don't collide in list/admin views.

const serviceKeyPrefix = "service_key_"

// KnownServiceSecrets maps secret names surfaced by skills (the env var
// names declared in SKILL.md's required_secrets) to the internal service
// provider id used for system-scope storage. When a user-scope secret is
// missing, buildSkillToolsFromRecords falls back to the system service key
// under this mapping.
var KnownServiceSecrets = map[string]string{
	"BRAVE_API_KEY": "brave_search",
}

// GetServiceKey returns the plaintext key for a service provider, preferring
// the user scope and falling back to system scope.
func (db *DB) GetServiceKey(provider, userID string) (string, error) {
	name := serviceKeyPrefix + provider
	if userID != "" {
		secret, err := db.GetSecret(userID, name)
		if err == nil {
			plaintext, err := crypto.Decrypt(secret.EncryptedValue)
			if err == nil && plaintext != "" {
				return plaintext, nil
			}
		}
	}
	secret, err := db.GetSecret("system", name)
	if err != nil {
		return "", sql.ErrNoRows
	}
	plaintext, err := crypto.Decrypt(secret.EncryptedValue)
	if err != nil {
		return "", fmt.Errorf("decrypt service key: %w", err)
	}
	return plaintext, nil
}

// SetServiceKey persists an encrypted service key under the given scope.
func (db *DB) SetServiceKey(provider, scope, value string) error {
	name := serviceKeyPrefix + provider
	encrypted, err := crypto.Encrypt(value)
	if err != nil {
		return fmt.Errorf("encrypt service key: %w", err)
	}
	return db.SaveSecret(uuid.New().String(), scope, name, encrypted)
}

// DeleteServiceKey removes a service key from the given scope.
func (db *DB) DeleteServiceKey(provider, scope string) error {
	return db.DeleteSecret(scope, serviceKeyPrefix+provider)
}

// ServiceKeyInfo summarizes a stored service key for list views.
type ServiceKeyInfo struct {
	Provider  string `json:"provider"`
	MaskedKey string `json:"masked_key"`
	UpdatedAt string `json:"updated_at"`
}

// ListServiceKeys returns all service keys under a scope with masked values.
func (db *DB) ListServiceKeys(scope string) ([]ServiceKeyInfo, error) {
	query := `SELECT name, encrypted_value, updated_at FROM secrets WHERE user = ? AND name LIKE ? ORDER BY name`
	rows, err := db.conn.Query(query, scope, serviceKeyPrefix+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ServiceKeyInfo
	for rows.Next() {
		var name, encValue string
		var updatedAt sql.NullString
		if err := rows.Scan(&name, &encValue, &updatedAt); err != nil {
			continue
		}
		provider := name[len(serviceKeyPrefix):]
		plain, err := crypto.Decrypt(encValue)
		masked := "****"
		if err == nil {
			masked = maskAPIKey(plain)
		}
		ts := ""
		if updatedAt.Valid {
			ts = updatedAt.String
		}
		out = append(out, ServiceKeyInfo{
			Provider:  provider,
			MaskedKey: masked,
			UpdatedAt: ts,
		})
	}
	return out, nil
}

// --- Usage tracking ---

// initServiceUsageTable creates the append-only usage log.
func (db *DB) initServiceUsageTable() error {
	query := `
	CREATE TABLE IF NOT EXISTS service_usage (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id TEXT NOT NULL,
		provider TEXT NOT NULL,
		units INTEGER NOT NULL DEFAULT 1,
		called_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_service_usage_provider_time ON service_usage(provider, called_at);
	CREATE INDEX IF NOT EXISTS idx_service_usage_user ON service_usage(user_id, called_at);`
	_, err := db.conn.Exec(query)
	return err
}

// RecordServiceUsage appends a usage row for the given provider and user.
func (db *DB) RecordServiceUsage(userID, provider string, units int) error {
	if units <= 0 {
		units = 1
	}
	_, err := db.conn.Exec(
		`INSERT INTO service_usage (user_id, provider, units) VALUES (?, ?, ?)`,
		userID, provider, units,
	)
	return err
}

// ServiceUsageStats aggregates usage for a single provider.
type ServiceUsageStats struct {
	Provider    string `json:"provider"`
	TotalCalls  int    `json:"total_calls"`
	TotalUnits  int    `json:"total_units"`
	MonthCalls  int    `json:"month_calls"`
	MonthUnits  int    `json:"month_units"`
	LastCalled  string `json:"last_called_at,omitempty"`
}

// GetServiceUsageStats returns lifetime and current-calendar-month totals.
func (db *DB) GetServiceUsageStats(provider string) (*ServiceUsageStats, error) {
	stats := &ServiceUsageStats{Provider: provider}
	now := time.Now().UTC()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).Format("2006-01-02 15:04:05")

	var lastCalled sql.NullString
	err := db.conn.QueryRow(
		`SELECT COUNT(*), COALESCE(SUM(units), 0), MAX(called_at) FROM service_usage WHERE provider = ?`,
		provider,
	).Scan(&stats.TotalCalls, &stats.TotalUnits, &lastCalled)
	if err != nil {
		return nil, fmt.Errorf("query total usage: %w", err)
	}
	if lastCalled.Valid {
		stats.LastCalled = lastCalled.String
	}

	err = db.conn.QueryRow(
		`SELECT COUNT(*), COALESCE(SUM(units), 0) FROM service_usage WHERE provider = ? AND called_at >= ?`,
		provider, monthStart,
	).Scan(&stats.MonthCalls, &stats.MonthUnits)
	if err != nil {
		return nil, fmt.Errorf("query month usage: %w", err)
	}
	return stats, nil
}

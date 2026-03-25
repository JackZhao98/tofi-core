package storage

import (
	"database/sql"
	"time"
)

// RefreshTokenRecord represents a stored refresh token.
type RefreshTokenRecord struct {
	ID        string
	UserID    string
	TokenHash string
	ExpiresAt string
	CreatedAt string
}

func (db *DB) initRefreshTokensTable() error {
	_, err := db.conn.Exec(`
	CREATE TABLE IF NOT EXISTS refresh_tokens (
		id         TEXT PRIMARY KEY,
		user_id    TEXT NOT NULL,
		token_hash TEXT NOT NULL UNIQUE,
		expires_at DATETIME NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE UNIQUE INDEX IF NOT EXISTS idx_refresh_tokens_hash ON refresh_tokens(token_hash);
	CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user ON refresh_tokens(user_id);
	`)
	return err
}

// CreateRefreshToken stores a new refresh token record.
func (db *DB) CreateRefreshToken(id, userID, tokenHash string, expiresAt time.Time) error {
	_, err := db.conn.Exec(
		`INSERT INTO refresh_tokens (id, user_id, token_hash, expires_at) VALUES (?, ?, ?, ?)`,
		id, userID, tokenHash, expiresAt.UTC().Format(time.RFC3339),
	)
	return err
}

// GetRefreshTokenByHash looks up a refresh token by its SHA-256 hash.
func (db *DB) GetRefreshTokenByHash(tokenHash string) (*RefreshTokenRecord, error) {
	var r RefreshTokenRecord
	err := db.conn.QueryRow(
		`SELECT id, user_id, token_hash, expires_at, created_at FROM refresh_tokens WHERE token_hash = ?`,
		tokenHash,
	).Scan(&r.ID, &r.UserID, &r.TokenHash, &r.ExpiresAt, &r.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// DeleteRefreshToken removes a single refresh token by ID.
func (db *DB) DeleteRefreshToken(id string) error {
	_, err := db.conn.Exec(`DELETE FROM refresh_tokens WHERE id = ?`, id)
	return err
}

// DeleteAllRefreshTokens removes all refresh tokens for a user.
func (db *DB) DeleteAllRefreshTokens(userID string) error {
	_, err := db.conn.Exec(`DELETE FROM refresh_tokens WHERE user_id = ?`, userID)
	return err
}

// CleanupExpiredRefreshTokens removes all expired refresh tokens.
func (db *DB) CleanupExpiredRefreshTokens() (int64, error) {
	result, err := db.conn.Exec(`DELETE FROM refresh_tokens WHERE expires_at < ?`,
		time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	n, _ := result.RowsAffected()
	return n, nil
}

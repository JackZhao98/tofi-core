package storage

import (
	"database/sql"
	"fmt"
	"log"
)

// AppEnvVarRecord mirrors SecretRecord but keyed by (app_id, name) instead
// of (user, name). The app_id scopes the override; user_id is kept for
// ownership checks and per-user analytics.
//
// Storage is intentionally separate from the `secrets` table because the
// existing `UNIQUE(user, name)` constraint there cannot be relaxed without
// a destructive migration. Two apps belonging to the same user could
// otherwise collide on the same env var name.
type AppEnvVarRecord struct {
	ID             string
	AppID          string
	UserID         string
	Name           string
	EncryptedValue string
	CreatedAt      sql.NullString
	UpdatedAt      sql.NullString
}

func (db *DB) initAppEnvVarsTable() error {
	q := `
	CREATE TABLE IF NOT EXISTS app_env_vars (
		id TEXT PRIMARY KEY,
		app_id TEXT NOT NULL,
		user_id TEXT NOT NULL,
		name TEXT NOT NULL,
		encrypted_value TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(app_id, name)
	);`
	if _, err := db.conn.Exec(q); err != nil {
		return fmt.Errorf("create app_env_vars table: %w", err)
	}
	return nil
}

// SaveAppEnvVar inserts or updates a per-app env var (encrypted value).
func (db *DB) SaveAppEnvVar(id, appID, userID, name, encryptedValue string) error {
	query := `
	INSERT INTO app_env_vars (id, app_id, user_id, name, encrypted_value, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	ON CONFLICT(app_id, name) DO UPDATE SET
		encrypted_value = excluded.encrypted_value,
		updated_at = CURRENT_TIMESTAMP;`
	if _, err := db.conn.Exec(query, id, appID, userID, name, encryptedValue); err != nil {
		return fmt.Errorf("save app env var: %w", err)
	}
	return nil
}

// GetAppEnvVar fetches a single (app_id, name) row including its encrypted value.
func (db *DB) GetAppEnvVar(appID, name string) (*AppEnvVarRecord, error) {
	q := `SELECT id, app_id, user_id, name, encrypted_value, created_at, updated_at
		FROM app_env_vars WHERE app_id = ? AND name = ?`
	row := db.conn.QueryRow(q, appID, name)
	var r AppEnvVarRecord
	if err := row.Scan(&r.ID, &r.AppID, &r.UserID, &r.Name, &r.EncryptedValue, &r.CreatedAt, &r.UpdatedAt); err != nil {
		return nil, err
	}
	return &r, nil
}

// ListAppEnvVars returns metadata (no encrypted value) for an app's env vars.
func (db *DB) ListAppEnvVars(appID string) ([]*AppEnvVarRecord, error) {
	q := `SELECT id, app_id, user_id, name, created_at, updated_at
		FROM app_env_vars WHERE app_id = ? ORDER BY name`
	rows, err := db.conn.Query(q, appID)
	if err != nil {
		return nil, fmt.Errorf("list app env vars: %w", err)
	}
	defer rows.Close()

	var out []*AppEnvVarRecord
	for rows.Next() {
		var r AppEnvVarRecord
		if err := rows.Scan(&r.ID, &r.AppID, &r.UserID, &r.Name, &r.CreatedAt, &r.UpdatedAt); err != nil {
			log.Printf("⚠️  scan app env var row: %v", err)
			continue
		}
		out = append(out, &r)
	}
	return out, nil
}

// LoadAppEnvVars returns a decrypted name→plaintext map ready to inject as
// process environment for a sandbox run. Decryption errors per-row are
// logged and skipped so a single corrupt entry can't kill an entire run.
func (db *DB) LoadAppEnvVars(appID string, decrypt func(string) (string, error)) (map[string]string, error) {
	q := `SELECT name, encrypted_value FROM app_env_vars WHERE app_id = ?`
	rows, err := db.conn.Query(q, appID)
	if err != nil {
		return nil, fmt.Errorf("load app env vars: %w", err)
	}
	defer rows.Close()

	out := make(map[string]string)
	for rows.Next() {
		var name, encrypted string
		if err := rows.Scan(&name, &encrypted); err != nil {
			log.Printf("⚠️  scan app env var row: %v", err)
			continue
		}
		plain, err := decrypt(encrypted)
		if err != nil {
			log.Printf("⚠️  decrypt app env var %s: %v", name, err)
			continue
		}
		out[name] = plain
	}
	return out, nil
}

// DeleteAppEnvVar removes a single (app_id, name) row.
func (db *DB) DeleteAppEnvVar(appID, name string) error {
	q := `DELETE FROM app_env_vars WHERE app_id = ? AND name = ?`
	res, err := db.conn.Exec(q, appID, name)
	if err != nil {
		return fmt.Errorf("delete app env var: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteAppEnvVarsForApp removes every env var attached to an app — called
// when the app itself is deleted to keep the table clean.
func (db *DB) DeleteAppEnvVarsForApp(appID string) error {
	q := `DELETE FROM app_env_vars WHERE app_id = ?`
	if _, err := db.conn.Exec(q, appID); err != nil {
		return fmt.Errorf("delete app env vars for app: %w", err)
	}
	return nil
}

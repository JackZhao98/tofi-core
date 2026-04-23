package storage

import "database/sql"

type ToolRuntimeInventoryItem struct {
	ID          string `json:"id"`
	ScopeType   string `json:"scope_type"` // shared | user
	OwnerID     string `json:"owner_id"`   // "shared" or username
	Category    string `json:"category"`
	Name        string `json:"name"`
	DisplayPath string `json:"display_path"`
	FileType    string `json:"file_type"`
	SizeBytes   int64  `json:"size_bytes"`
	IsDir       bool   `json:"is_dir"`
	LastUsedAt  string `json:"last_used_at"`
	UpdatedAt   string `json:"updated_at"`
}

func (db *DB) initToolRuntimeInventoryTable() error {
	_, err := db.conn.Exec(`
	CREATE TABLE IF NOT EXISTS tool_runtime_inventory (
		id TEXT PRIMARY KEY,
		scope_type TEXT NOT NULL,
		owner_id TEXT NOT NULL,
		category TEXT NOT NULL,
		name TEXT NOT NULL,
		display_path TEXT NOT NULL,
		file_type TEXT NOT NULL,
		size_bytes INTEGER NOT NULL DEFAULT 0,
		is_dir INTEGER NOT NULL DEFAULT 0,
		last_used_at DATETIME,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_tool_runtime_scope_owner ON tool_runtime_inventory(scope_type, owner_id);
	CREATE INDEX IF NOT EXISTS idx_tool_runtime_owner_size ON tool_runtime_inventory(owner_id, size_bytes DESC);
	`)
	return err
}

func (db *DB) ReplaceToolRuntimeInventory(scopeType, ownerID string, items []ToolRuntimeInventoryItem) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM tool_runtime_inventory WHERE scope_type = ? AND owner_id = ?`, scopeType, ownerID); err != nil {
		return err
	}

	stmt, err := tx.Prepare(`
		INSERT INTO tool_runtime_inventory
		(id, scope_type, owner_id, category, name, display_path, file_type, size_bytes, is_dir, last_used_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, item := range items {
		isDir := 0
		if item.IsDir {
			isDir = 1
		}
		if _, err := stmt.Exec(item.ID, scopeType, ownerID, item.Category, item.Name, item.DisplayPath, item.FileType, item.SizeBytes, isDir, item.LastUsedAt); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (db *DB) ListToolRuntimeInventory(scopeType, ownerID string) ([]ToolRuntimeInventoryItem, error) {
	rows, err := db.conn.Query(`
		SELECT id, scope_type, owner_id, category, name, display_path, file_type, size_bytes, is_dir, COALESCE(last_used_at, ''), COALESCE(updated_at, '')
		FROM tool_runtime_inventory
		WHERE scope_type = ? AND owner_id = ?
		ORDER BY last_used_at DESC, size_bytes DESC, display_path ASC
	`, scopeType, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []ToolRuntimeInventoryItem
	for rows.Next() {
		var item ToolRuntimeInventoryItem
		var isDir int
		if err := rows.Scan(&item.ID, &item.ScopeType, &item.OwnerID, &item.Category, &item.Name, &item.DisplayPath, &item.FileType, &item.SizeBytes, &isDir, &item.LastUsedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.IsDir = isDir != 0
		items = append(items, item)
	}
	return items, nil
}

func (db *DB) GetToolRuntimeTotalBytes(scopeType, ownerID string) (int64, error) {
	var total sql.NullInt64
	err := db.conn.QueryRow(`
		SELECT COALESCE(SUM(size_bytes), 0)
		FROM tool_runtime_inventory
		WHERE scope_type = ? AND owner_id = ?
	`, scopeType, ownerID).Scan(&total)
	if err != nil {
		return 0, err
	}
	if !total.Valid {
		return 0, nil
	}
	return total.Int64, nil
}

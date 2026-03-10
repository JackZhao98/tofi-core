package storage

import (
	"fmt"
	"strings"
	"time"
)

// Memory represents a stored memory entry.
type Memory struct {
	ID        int64  `json:"id"`
	UserID    string `json:"user_id"`
	Content   string `json:"content"`
	Tags      string `json:"tags"`
	Source    string `json:"source"`  // "agent" (explicit save) or "auto" (auto-extracted)
	CardID    string `json:"card_id"` // Associated Kanban card ID (optional)
	CreatedAt string `json:"created_at"`
}

// initMemoriesTable creates the memories table and FTS5 virtual table for full-text search.
func (db *DB) initMemoriesTable() error {
	// Main table
	_, err := db.conn.Exec(`
		CREATE TABLE IF NOT EXISTS memories (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id    TEXT    NOT NULL,
			content    TEXT    NOT NULL,
			tags       TEXT    DEFAULT '',
			source     TEXT    DEFAULT 'agent',
			card_id    TEXT    DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_memories_user ON memories(user_id);
	`)
	if err != nil {
		return fmt.Errorf("create memories table: %w", err)
	}

	// FTS5 virtual table for full-text search with BM25 ranking.
	// content=memories syncs with the main table via triggers.
	_, err = db.conn.Exec(`
		CREATE VIRTUAL TABLE IF NOT EXISTS memories_fts USING fts5(
			content,
			tags,
			content=memories,
			content_rowid=id
		);
	`)
	if err != nil {
		return fmt.Errorf("create memories_fts: %w", err)
	}

	// Triggers to keep FTS index in sync with main table
	db.conn.Exec(`
		CREATE TRIGGER IF NOT EXISTS memories_ai AFTER INSERT ON memories BEGIN
			INSERT INTO memories_fts(rowid, content, tags) VALUES (new.id, new.content, new.tags);
		END;
	`)
	db.conn.Exec(`
		CREATE TRIGGER IF NOT EXISTS memories_ad AFTER DELETE ON memories BEGIN
			INSERT INTO memories_fts(memories_fts, rowid, content, tags) VALUES('delete', old.id, old.content, old.tags);
		END;
	`)
	db.conn.Exec(`
		CREATE TRIGGER IF NOT EXISTS memories_au AFTER UPDATE ON memories BEGIN
			INSERT INTO memories_fts(memories_fts, rowid, content, tags) VALUES('delete', old.id, old.content, old.tags);
			INSERT INTO memories_fts(rowid, content, tags) VALUES (new.id, new.content, new.tags);
		END;
	`)

	return nil
}

// SaveMemory stores a new memory entry and returns its ID.
func (db *DB) SaveMemory(userID, content, tags, source, cardID string) (int64, error) {
	if content == "" {
		return 0, fmt.Errorf("memory content cannot be empty")
	}
	if source == "" {
		source = "agent"
	}

	result, err := db.conn.Exec(
		`INSERT INTO memories (user_id, content, tags, source, card_id) VALUES (?, ?, ?, ?, ?)`,
		userID, content, tags, source, cardID,
	)
	if err != nil {
		return 0, fmt.Errorf("save memory: %w", err)
	}
	return result.LastInsertId()
}

// RecallMemories searches memories using FTS5 full-text search with BM25 ranking.
// The query string supports FTS5 syntax (e.g., "python AND web" or "user preference").
func (db *DB) RecallMemories(userID, query string, limit int) ([]Memory, error) {
	if limit <= 0 {
		limit = 5
	}
	if limit > 50 {
		limit = 50
	}

	// Sanitize query for FTS5: wrap each word in quotes to avoid syntax errors
	query = sanitizeFTSQuery(query)
	if query == "" {
		return nil, nil
	}

	rows, err := db.conn.Query(`
		SELECT m.id, m.user_id, m.content, m.tags, m.source, m.card_id, m.created_at
		FROM memories_fts
		JOIN memories m ON m.id = memories_fts.rowid
		WHERE memories_fts MATCH ? AND m.user_id = ?
		ORDER BY bm25(memories_fts)
		LIMIT ?
	`, query, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("recall memories: %w", err)
	}
	defer rows.Close()

	var memories []Memory
	for rows.Next() {
		var m Memory
		if err := rows.Scan(&m.ID, &m.UserID, &m.Content, &m.Tags, &m.Source, &m.CardID, &m.CreatedAt); err != nil {
			continue
		}
		memories = append(memories, m)
	}
	return memories, nil
}

// ListMemories returns memories for a user ordered by most recent first.
func (db *DB) ListMemories(userID string, limit, offset int) ([]Memory, error) {
	if limit <= 0 {
		limit = 20
	}

	rows, err := db.conn.Query(
		`SELECT id, user_id, content, tags, source, card_id, created_at
		 FROM memories WHERE user_id = ? ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		userID, limit, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("list memories: %w", err)
	}
	defer rows.Close()

	var memories []Memory
	for rows.Next() {
		var m Memory
		if err := rows.Scan(&m.ID, &m.UserID, &m.Content, &m.Tags, &m.Source, &m.CardID, &m.CreatedAt); err != nil {
			continue
		}
		memories = append(memories, m)
	}
	return memories, nil
}

// DeleteMemory removes a memory entry by ID (must belong to the user).
func (db *DB) DeleteMemory(userID string, id int64) error {
	result, err := db.conn.Exec(`DELETE FROM memories WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return fmt.Errorf("delete memory: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("memory not found")
	}
	return nil
}

// CountMemories returns the total number of memories for a user.
func (db *DB) CountMemories(userID string) (int, error) {
	var count int
	err := db.conn.QueryRow(`SELECT COUNT(*) FROM memories WHERE user_id = ?`, userID).Scan(&count)
	return count, err
}

// sanitizeFTSQuery converts a plain text query into a safe FTS5 query.
// Each word is quoted to prevent FTS5 syntax errors from special characters.
func sanitizeFTSQuery(query string) string {
	query = strings.TrimSpace(query)
	if query == "" {
		return ""
	}

	words := strings.Fields(query)
	var parts []string
	for _, w := range words {
		// Remove any existing quotes and wrap in double quotes
		w = strings.ReplaceAll(w, `"`, "")
		if w != "" {
			parts = append(parts, `"`+w+`"`)
		}
	}

	if len(parts) == 0 {
		return ""
	}

	// Join with OR so any matching word returns results
	return strings.Join(parts, " OR ")
}

// FormatMemoriesForAgent formats a list of memories into a human-readable string for the agent.
func FormatMemoriesForAgent(memories []Memory) string {
	if len(memories) == 0 {
		return "No relevant memories found."
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d relevant memories:\n", len(memories)))
	for i, m := range memories {
		// Parse and format the time
		t, err := time.Parse("2006-01-02 15:04:05", m.CreatedAt)
		timeStr := m.CreatedAt
		if err == nil {
			timeStr = t.Format("Jan 2, 2006")
		}

		sb.WriteString(fmt.Sprintf("\n[%d] (%s", i+1, timeStr))
		if m.Tags != "" {
			sb.WriteString(fmt.Sprintf(", tags: %s", m.Tags))
		}
		sb.WriteString(fmt.Sprintf(")\n%s\n", m.Content))
	}
	return sb.String()
}

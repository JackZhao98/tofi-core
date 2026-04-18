package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// AgentRunEvent is a lightweight audit row written once per agent loop start.
// It's the single source of truth for the "runs/day" quota and unifies every
// entry point: chat user messages, webhook triggers, manual Run Now, and the
// cron scheduler. We keep the existing app_runs table for rich per-app
// history; this table is just for counting.
type AgentRunEvent struct {
	ID        string
	UserID    string
	Source    string // "chat" | "app" | "schedule" | "webhook" | "api"
	CreatedAt string
}

// initAgentRunsTable creates the agent_runs table + an index on (user_id,
// created_at) so the daily-count query stays O(1) on growing data.
func (db *DB) initAgentRunsTable() error {
	_, err := db.conn.Exec(`
		CREATE TABLE IF NOT EXISTS agent_runs (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			source TEXT NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_agent_runs_user_date ON agent_runs(user_id, created_at);
	`)
	if err != nil {
		return fmt.Errorf("init agent_runs table: %w", err)
	}
	return nil
}

// RecordAgentRun inserts a new row. Call this once per dispatched run,
// right at the moment the quota gate allowed it through. Source should be
// one of the documented values. Failures are returned but callers often
// choose to log + continue — we'd rather let the agent execute and miss a
// quota data point than hard-fail the whole request because a logging
// INSERT failed.
func (db *DB) RecordAgentRun(userID, source string) error {
	if userID == "" {
		return errors.New("userID required")
	}
	if source == "" {
		source = "unknown"
	}
	_, err := db.conn.Exec(`
		INSERT INTO agent_runs(id, user_id, source) VALUES(?, ?, ?)
	`, uuid.New().String(), userID, source)
	if err != nil {
		return fmt.Errorf("record agent run: %w", err)
	}
	return nil
}

// CountDailyAgentRuns returns the number of agent runs dispatched for this
// user today (UTC). This is the canonical number backing the "Daily Runs
// X/Y" meter in the UI and the quota gate.
func (db *DB) CountDailyAgentRuns(userID string) (int, error) {
	today := time.Now().UTC().Format("2006-01-02")
	var count int
	err := db.conn.QueryRow(`
		SELECT COUNT(*) FROM agent_runs
		WHERE user_id = ? AND DATE(created_at) = ?
	`, userID, today).Scan(&count)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("count daily agent runs: %w", err)
	}
	return count, nil
}

// CountDailyAgentRunsBySource breaks down today's runs by source. Admin
// dashboard uses this to show "chat: 12, app: 4, schedule: 2" etc.
func (db *DB) CountDailyAgentRunsBySource(userID string) (map[string]int, error) {
	today := time.Now().UTC().Format("2006-01-02")
	rows, err := db.conn.Query(`
		SELECT source, COUNT(*) FROM agent_runs
		WHERE user_id = ? AND DATE(created_at) = ?
		GROUP BY source
	`, userID, today)
	if err != nil {
		return nil, fmt.Errorf("count daily agent runs by source: %w", err)
	}
	defer rows.Close()
	out := make(map[string]int)
	for rows.Next() {
		var source string
		var count int
		if err := rows.Scan(&source, &count); err != nil {
			return nil, err
		}
		out[source] = count
	}
	return out, nil
}

package storage

import (
	"fmt"
	"log"
)

// AgentRecord represents a pre-configured Agent (a "pre-orchestrated Wish")
type AgentRecord struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Description      string `json:"description"`
	Prompt           string `json:"prompt"`            // one-line task prompt
	SystemPrompt     string `json:"system_prompt"`     // custom system prompt (empty = default)
	Model            string `json:"model"`             // empty = auto-detect
	Skills           string `json:"skills"`            // JSON array of skill IDs
	ScheduleRules    string `json:"schedule_rules"`    // JSON ScheduleRule
	Capabilities     string `json:"capabilities"`      // JSON: capability config (mcp_servers, web_search, notify, etc.)
	BufferSize       int    `json:"buffer_size"`       // max pending runs
	RenewalThreshold int    `json:"renewal_threshold"` // renew when pending < this
	IsActive         bool   `json:"is_active"`
	UserID           string `json:"user_id"`
	CreatedAt        string `json:"created_at"`
	UpdatedAt        string `json:"updated_at"`
}

func (db *DB) initAgentsTable() error {
	agentsQuery := `
	CREATE TABLE IF NOT EXISTS agents (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		description TEXT DEFAULT '',
		prompt TEXT DEFAULT '',
		system_prompt TEXT DEFAULT '',
		model TEXT DEFAULT '',
		skills TEXT DEFAULT '[]',
		schedule_rules TEXT DEFAULT '[]',
		buffer_size INTEGER DEFAULT 20,
		renewal_threshold INTEGER DEFAULT 5,
		is_active INTEGER DEFAULT 0,
		user_id TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_agents_user ON agents(user_id);
	`
	if _, err := db.conn.Exec(agentsQuery); err != nil {
		return fmt.Errorf("create agents table: %w", err)
	}

	// Migration: add capabilities column
	db.conn.Exec("ALTER TABLE agents ADD COLUMN capabilities TEXT DEFAULT '{}'")

	// Enable foreign key support
	if _, err := db.conn.Exec("PRAGMA foreign_keys = ON"); err != nil {
		log.Printf("⚠️  Failed to enable foreign keys: %v", err)
	}

	return nil
}

// ── Agent CRUD ──

func (db *DB) CreateAgent(agent *AgentRecord) error {
	if agent.Capabilities == "" {
		agent.Capabilities = "{}"
	}
	query := `INSERT INTO agents (id, name, description, prompt, system_prompt, model, skills, schedule_rules, capabilities, buffer_size, renewal_threshold, is_active, user_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`
	_, err := db.conn.Exec(query,
		agent.ID, agent.Name, agent.Description, agent.Prompt, agent.SystemPrompt,
		agent.Model, agent.Skills, agent.ScheduleRules, agent.Capabilities,
		agent.BufferSize, agent.RenewalThreshold, boolToInt(agent.IsActive), agent.UserID,
	)
	return err
}

func (db *DB) GetAgent(id string) (*AgentRecord, error) {
	query := `SELECT id, name, COALESCE(description,''), COALESCE(prompt,''), COALESCE(system_prompt,''),
		COALESCE(model,''), COALESCE(skills,'[]'), COALESCE(schedule_rules,'[]'), COALESCE(capabilities,'{}'),
		buffer_size, renewal_threshold, is_active, user_id, created_at, updated_at
		FROM agents WHERE id = ?`
	row := db.conn.QueryRow(query, id)
	var a AgentRecord
	var isActive int
	if err := row.Scan(&a.ID, &a.Name, &a.Description, &a.Prompt, &a.SystemPrompt,
		&a.Model, &a.Skills, &a.ScheduleRules, &a.Capabilities,
		&a.BufferSize, &a.RenewalThreshold, &isActive, &a.UserID, &a.CreatedAt, &a.UpdatedAt,
	); err != nil {
		return nil, err
	}
	a.IsActive = isActive != 0
	return &a, nil
}

func (db *DB) ListAgents(userID string) ([]*AgentRecord, error) {
	query := `SELECT id, name, COALESCE(description,''), COALESCE(prompt,''), COALESCE(system_prompt,''),
		COALESCE(model,''), COALESCE(skills,'[]'), COALESCE(schedule_rules,'[]'), COALESCE(capabilities,'{}'),
		buffer_size, renewal_threshold, is_active, user_id, created_at, updated_at
		FROM agents WHERE user_id = ? ORDER BY updated_at DESC`
	rows, err := db.conn.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var agents []*AgentRecord
	for rows.Next() {
		var a AgentRecord
		var isActive int
		if err := rows.Scan(&a.ID, &a.Name, &a.Description, &a.Prompt, &a.SystemPrompt,
			&a.Model, &a.Skills, &a.ScheduleRules, &a.Capabilities,
			&a.BufferSize, &a.RenewalThreshold, &isActive, &a.UserID, &a.CreatedAt, &a.UpdatedAt,
		); err != nil {
			return nil, err
		}
		a.IsActive = isActive != 0
		agents = append(agents, &a)
	}
	return agents, nil
}

func (db *DB) UpdateAgent(agent *AgentRecord) error {
	if agent.Capabilities == "" {
		agent.Capabilities = "{}"
	}
	query := `UPDATE agents SET name = ?, description = ?, prompt = ?, system_prompt = ?,
		model = ?, skills = ?, schedule_rules = ?, capabilities = ?, buffer_size = ?, renewal_threshold = ?,
		is_active = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND user_id = ?`
	result, err := db.conn.Exec(query,
		agent.Name, agent.Description, agent.Prompt, agent.SystemPrompt,
		agent.Model, agent.Skills, agent.ScheduleRules, agent.Capabilities,
		agent.BufferSize, agent.RenewalThreshold, boolToInt(agent.IsActive),
		agent.ID, agent.UserID,
	)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("agent not found or not owned by user")
	}
	return nil
}

func (db *DB) DeleteAgent(id, userID string) error {
	result, err := db.conn.Exec(`DELETE FROM agents WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("agent not found or not owned by user")
	}
	return nil
}

func (db *DB) SetAgentActive(id, userID string, active bool) error {
	result, err := db.conn.Exec(`UPDATE agents SET is_active = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND user_id = ?`,
		boolToInt(active), id, userID)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("agent not found or not owned by user")
	}
	return nil
}

func (db *DB) ListActiveAgents() ([]*AgentRecord, error) {
	query := `SELECT id, name, COALESCE(description,''), COALESCE(prompt,''), COALESCE(system_prompt,''),
		COALESCE(model,''), COALESCE(skills,'[]'), COALESCE(schedule_rules,'[]'), COALESCE(capabilities,'{}'),
		buffer_size, renewal_threshold, is_active, user_id, created_at, updated_at
		FROM agents WHERE is_active = 1`
	rows, err := db.conn.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var agents []*AgentRecord
	for rows.Next() {
		var a AgentRecord
		var isActive int
		if err := rows.Scan(&a.ID, &a.Name, &a.Description, &a.Prompt, &a.SystemPrompt,
			&a.Model, &a.Skills, &a.ScheduleRules, &a.Capabilities,
			&a.BufferSize, &a.RenewalThreshold, &isActive, &a.UserID, &a.CreatedAt, &a.UpdatedAt,
		); err != nil {
			return nil, err
		}
		a.IsActive = isActive != 0
		agents = append(agents, &a)
	}
	return agents, nil
}

// helper
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

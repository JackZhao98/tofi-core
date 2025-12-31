package storage

import (
	"database/sql"
	"path/filepath"

	_ "modernc.org/sqlite"
)

type ExecutionRecord struct {
	ID           string
	WorkflowName string
	User         string
	Status       string
	StateJSON    string // 中间状态
	ResultJSON   string // 最终报告
	CreatedAt    string
}

type DB struct {
	conn *sql.DB
}

func InitDB(homeDir string) (*DB, error) {
	dbPath := filepath.Join(homeDir, "tofi.db")
	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	// 增加 state_json 列
	query := `
	CREATE TABLE IF NOT EXISTS executions (
		id TEXT PRIMARY KEY,
		workflow_name TEXT,
		user TEXT,
		status TEXT,
		state_json TEXT,
		result_json TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`
	if _, err := conn.Exec(query); err != nil {
		return nil, err
	}

	return &DB{conn: conn}, nil
}

func (db *DB) Close() error {
	return db.conn.Close()
}

// SaveExecution 既可以保存中间状态，也可以保存最终结果 (使用 REPLACE INTO)
func (db *DB) SaveExecution(id, name, user, status, stateJSON, resultJSON string) error {
	query := `
	INSERT OR REPLACE INTO executions (id, workflow_name, user, status, state_json, result_json, created_at)
	VALUES (?, ?, ?, ?, ?, ?, (SELECT created_at FROM executions WHERE id = ? OR CURRENT_TIMESTAMP));`
	
	// 注意：SQLite 的 REPLACE 会导致 created_at 丢失，所以我们用一个小技巧保留它
	_, err := db.conn.Exec(query, id, name, user, status, stateJSON, resultJSON, id)
	return err
}

func (db *DB) GetExecution(id string) (*ExecutionRecord, error) {
	row := db.conn.QueryRow("SELECT id, workflow_name, user, status, state_json, result_json, created_at FROM executions WHERE id = ?", id)
	var r ExecutionRecord
	err := row.Scan(&r.ID, &r.WorkflowName, &r.User, &r.Status, &r.StateJSON, &r.ResultJSON, &r.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &r, nil
}
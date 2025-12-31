package storage

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type ExecutionRecord struct {
	ID           string
	WorkflowName string
	Status       string
	StartTime    time.Time
	EndTime      time.Time
	Duration     string
	ResultJSON   string
}

type DB struct {
	conn *sql.DB
}

func InitDB(homeDir string) (*DB, error) {
	dbPath := filepath.Join(homeDir, "tofi.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %v", err)
	}

	// 创建表结构
	query := `
	CREATE TABLE IF NOT EXISTS executions (
		id TEXT PRIMARY KEY,
		workflow_name TEXT,
		status TEXT,
		start_time DATETIME,
		end_time DATETIME,
		duration TEXT,
		result_json TEXT
	);
	CREATE INDEX IF NOT EXISTS idx_status ON executions(status);
	`
	if _, err := db.Exec(query); err != nil {
		return nil, fmt.Errorf("failed to create tables: %v", err)
	}

	return &DB{conn: db}, nil
}

func (db *DB) Close() error {
	return db.conn.Close()
}

// SaveExecution 插入或更新执行记录
func (db *DB) SaveExecution(record *ExecutionRecord) error {
	query := `
	INSERT INTO executions (id, workflow_name, status, start_time, end_time, duration, result_json)
	VALUES (?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		status = excluded.status,
		end_time = excluded.end_time,
		duration = excluded.duration,
		result_json = excluded.result_json;
	`
	_, err := db.conn.Exec(query,
		record.ID,
		record.WorkflowName,
		record.Status,
		record.StartTime,
		record.EndTime,
		record.Duration,
		record.ResultJSON,
	)
	return err
}

// GetExecution 获取单条记录
func (db *DB) GetExecution(id string) (*ExecutionRecord, error) {
	row := db.conn.QueryRow("SELECT id, workflow_name, status, start_time, end_time, duration, result_json FROM executions WHERE id = ?", id)
	var r ExecutionRecord
	err := row.Scan(&r.ID, &r.WorkflowName, &r.Status, &r.StartTime, &r.EndTime, &r.Duration, &r.ResultJSON)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// ListExecutions 获取历史列表
func (db *DB) ListExecutions(limit int) ([]ExecutionRecord, error) {
	rows, err := db.conn.Query("SELECT id, workflow_name, status, start_time, end_time, duration FROM executions ORDER BY start_time DESC LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []ExecutionRecord
	for rows.Next() {
		var r ExecutionRecord
		if err := rows.Scan(&r.ID, &r.WorkflowName, &r.Status, &r.StartTime, &r.EndTime, &r.Duration); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, nil
}

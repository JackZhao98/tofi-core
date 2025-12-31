package engine

import (
	"encoding/json"
	"tofi-core/internal/models"
	"tofi-core/internal/storage"
)

// SaveState 保存工作流执行的中间状态到数据库
func SaveState(ctx *models.ExecutionContext) error {
	if ctx.DB == nil {
		return nil
	}
	db, ok := ctx.DB.(*storage.DB)
	if !ok {
		return nil
	}

	results, stats := ctx.Snapshot()
	state := models.ExecutionResult{
		ExecutionID:  ctx.ExecutionID,
		WorkflowName: ctx.WorkflowName,
		Status:       "RUNNING",
		Outputs:      results,
		Stats:        stats,
	}

	jb, _ := json.Marshal(state)
	return db.SaveExecution(ctx.ExecutionID, ctx.WorkflowName, ctx.User, "RUNNING", string(jb), "")
}

// LoadState 从数据库中恢复执行状态
func LoadState(execID string, db *storage.DB, homeDir string) (*models.ExecutionContext, error) {
	record, err := db.GetExecution(execID)
	if err != nil {
		return nil, err
	}

	var state models.ExecutionResult
	if err := json.Unmarshal([]byte(record.StateJSON), &state); err != nil {
		return nil, err
	}

	ctx := models.NewExecutionContext(execID, record.User, homeDir)
	ctx.WorkflowName = record.WorkflowName
	ctx.DB = db
	
	for k, v := range state.Outputs {
		ctx.SetResult(k, v)
	}
	ctx.Stats = state.Stats
	
	return ctx, nil
}

// SaveReport 将最终报告存入数据库
func SaveReport(wf *models.Workflow, ctx *models.ExecutionContext, db *storage.DB) error {
	if db == nil {
		return nil
	}

	results, stats := ctx.Snapshot()
	report := models.ExecutionResult{
		ExecutionID:  ctx.ExecutionID,
		WorkflowName: wf.Name,
		Status:       "COMPLETED",
		Stats:        stats,
		Outputs:      results,
	}

	jb, _ := json.Marshal(report)
	return db.SaveExecution(ctx.ExecutionID, wf.Name, ctx.User, "COMPLETED", "", string(jb))
}
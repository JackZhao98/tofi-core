package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"tofi-core/internal/models"
	"tofi-core/internal/storage"
)

var stateLock sync.Mutex

// StateData 定义中间状态文件的结构
type StateData struct {
	ExecutionID string            `json:"execution_id"`
	UpdateTime  time.Time         `json:"update_time"`
	Results     map[string]string `json:"results"`
}

// SaveState 将当前的运行状态写入 states/ 目录 (Snapshot)
func SaveState(ctx *models.ExecutionContext) error {
	stateLock.Lock()
	defer stateLock.Unlock()

	// 1. 确保目录存在
	if err := os.MkdirAll(ctx.Paths.States, 0755); err != nil {
		return err
	}

	// 2. 获取数据快照
	results, _ := ctx.Snapshot()

	state := StateData{
		ExecutionID: ctx.ExecutionID,
		UpdateTime:  time.Now(),
		Results:     results,
	}

	// 3. 写入文件
	filePath := filepath.Join(ctx.Paths.States, fmt.Sprintf("state-%s.json", ctx.ExecutionID))
	fileData, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filePath, fileData, 0644)
}

// LoadState 加载指定 ExecutionID 的状态
func LoadState(execID, homeDir string) (*models.ExecutionContext, error) {
	// 先创建 Context 以获取正确的路径配置
	ctx := models.NewExecutionContext(execID, homeDir)
	filePath := filepath.Join(ctx.Paths.States, fmt.Sprintf("state-%s.json", execID))

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("读取状态文件失败: %v", err)
	}

	var state StateData
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("解析状态文件失败: %v", err)
	}

	// 恢复 Results
	for k, v := range state.Results {
		if strings.HasPrefix(v, "ERR_PROPAGATION:") || strings.HasPrefix(v, "SKIPPED_BY:") {
			continue 
		}
		ctx.Results[k] = v
	}

	return ctx, nil
}

// SaveReport 将最终执行结果存入数据库
func SaveReport(wf *models.Workflow, ctx *models.ExecutionContext, db *storage.DB) error {
	results, stats := ctx.Snapshot()

	// 1. 计算整体状态
	overallStatus := "SUCCESS"
	startTime := time.Now() 
	if len(stats) > 0 {
		startTime = stats[0].StartTime
		for _, stat := range stats {
			if stat.Status == "ERROR" {
				overallStatus = "FAILED"
				break
			}
		}
	}

	// 2. 构造数据
	duration := time.Since(startTime).String()
	
	fullResult := models.ExecutionResult{
		ExecutionID:  ctx.ExecutionID,
		WorkflowName: wf.Name,
		Status:       overallStatus,
		StartTime:    startTime,
		EndTime:      time.Now(),
		Duration:     duration,
		Stats:        stats,
		Outputs:      results,
	}
	jb, _ := json.Marshal(fullResult)

	record := &storage.ExecutionRecord{
		ID:           ctx.ExecutionID,
		WorkflowName: wf.Name,
		Status:       overallStatus,
		StartTime:    startTime,
		EndTime:      time.Now(),
		Duration:     duration,
		ResultJSON:   string(jb),
	}

	// 3. 写入 DB
	if err := db.SaveExecution(record); err != nil {
		return fmt.Errorf("failed to save execution to db: %v", err)
	}

	// 4. 清理逻辑：如果整体成功，则删除中间状态文件
	if overallStatus == "SUCCESS" {
		statePath := filepath.Join(ctx.Paths.States, fmt.Sprintf("state-%s.json", ctx.ExecutionID))
		_ = os.Remove(statePath)
	}

	return nil
}
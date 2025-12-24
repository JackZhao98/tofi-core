package engine

import (
	"log"
	"time"
	"tofi-core/internal/models"
)

// RunNode 核心辐射引擎：负责并发调度与耗时统计
func RunNode(wf *models.Workflow, nodeID string, ctx *models.ExecutionContext) {
	// 1. 【生命周期管理】
	// 无论以何种方式退出，都要释放计数
	defer ctx.Wg.Done()

	node, exists := wf.Nodes[nodeID]
	// 【防重拦截】：防止 Root 扫描和 Next 辐射导致的重复运行
	if !exists || ctx.CheckAndSetStarted(nodeID) {
		return
	}

	// 2. 【依赖检查】
	for _, depID := range node.Dependencies {
		if _, completed := ctx.GetResult(depID); !completed {
			log.Printf("[%s] [WAIT]    [%s] 仍在等待依赖: %s", ctx.ExecutionID, node.ID, depID)
			return
		}
	}

	// 3. 【执行准备】
	action := GetAction(node.Type)
	log.Printf("[%s] [START]   [%s] 类型: %s", ctx.ExecutionID, node.ID, node.Type)

	// --- 开始计时 ---
	startTime := time.Now()
	var res string
	var err error

	// 4. 【执行阶段】 包含重试逻辑
	for i := 0; i <= node.RetryCount; i++ {
		if i > 0 {
			log.Printf("[%s] [RETRY]   [%s] 第 %d 次重试...", ctx.ExecutionID, node.ID, i)
		}
		res, err = action.Execute(node, ctx)
		if err == nil {
			break
		}
	}
	// --- 结束计时 ---
	duration := time.Since(startTime)

	// 5. 【数据记录】 构造统计快照（为可视化提供数据源）
	stat := models.NodeStat{
		NodeID:    node.ID,
		Type:      node.Type,
		StartTime: startTime,
		Duration:  duration,
	}

	// 6. 【结果分发】
	if err != nil {
		if err.Error() == "CONDITION_NOT_MET" {
			stat.Status = "SKIP"
			ctx.RecordStat(stat)
			log.Printf("[%s] [SKIP]    [%s] 条件未满足: %s", ctx.ExecutionID, node.ID, res)
			return
		}

		stat.Status = "ERROR"
		ctx.RecordStat(stat)
		log.Printf("[%s] [ERROR]   [%s] 执行失败: %v (表达式: %s)", ctx.ExecutionID, node.ID, err, res)
		for _, failNodeID := range node.OnFailure {
			ctx.Wg.Add(1)
			go RunNode(wf, failNodeID, ctx)
		}
		return
	}

	// 成功路径
	stat.Status = "SUCCESS"
	ctx.RecordStat(stat)
	ctx.SetResult(node.ID, res)

	// 如果是逻辑节点，日志输出 PASS 会更直观
	if node.Type == "if" {
		log.Printf("[%s] [PASS]    [%s] 逻辑通过: %s", ctx.ExecutionID, node.ID, res)
	} else {
		log.Printf("[%s] [SUCCESS] [%s] 输出: %s", ctx.ExecutionID, node.ID, res)
	}

	// 辐射后续节点
	for _, nextID := range node.Next {
		ctx.Wg.Add(1)
		go RunNode(wf, nextID, ctx)
	}
}

// GetAction 工厂函数
func GetAction(nodeType string) Action {
	switch nodeType {
	case "shell", "api":
		return &TaskAction{}
	case "if":
		return &LogicAction{}
	case "var", "const":
		return &VarAction{}
	default:
		return &VirtualAction{}
	}
}

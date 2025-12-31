package parser

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"tofi-core/internal/models"

	"gopkg.in/yaml.v3"
)

// LoadWorkflow 会根据文件后缀名自动选择解析方式
func LoadWorkflow(path string) (*models.Workflow, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	ext := filepath.Ext(path)
	return ParseWorkflowFromBytes(data, ext)
}

// ParseWorkflowFromBytes 从字节数组解析工作流 (支持 embed 等场景)
func ParseWorkflowFromBytes(data []byte, format string) (*models.Workflow, error) {
	var wf models.Workflow
	var err error

	// format 可以是 ".yaml", ".yml", ".json" 或 "yaml", "json"
	if format == ".yaml" || format == ".yml" || format == "yaml" || format == "yml" {
		err = yaml.Unmarshal(data, &wf)
	} else if format == ".json" || format == "json" {
		err = json.Unmarshal(data, &wf)
	} else {
		return nil, fmt.Errorf("不支持的文件格式: %s", format)
	}

	if err != nil {
		return nil, err
	}

	// 1. 注入 ID (Map Key -> Node.ID)
	for id, node := range wf.Nodes {
		node.ID = id
	}

	// 2. 自动计算依赖 (根据 Next 反向填充 Dependencies)
	for currentID, node := range wf.Nodes {
		for _, nextID := range node.Next {
			targetNode, ok := wf.Nodes[nextID]
			if !ok {
				return nil, fmt.Errorf("node '%s' points to non-existent next node '%s'", currentID, nextID)
			}

			// 检查重复依赖
			exists := false
			for _, dep := range targetNode.Dependencies {
				if dep == currentID {
					exists = true
					break
				}
			}
			if !exists {
				targetNode.Dependencies = append(targetNode.Dependencies, currentID)
			}
		}

		// 处理 OnFailure 跳转的依赖
		// 注意: OnFailure 的目标节点通常是错误处理节点，它也应该依赖于当前节点
		for _, failID := range node.OnFailure {
			targetNode, ok := wf.Nodes[failID]
			if !ok {
				return nil, fmt.Errorf("node '%s' points to non-existent on_failure node '%s'", currentID, failID)
			}
			exists := false
			for _, dep := range targetNode.Dependencies {
				if dep == currentID {
					exists = true
					break
				}
			}
			if !exists {
				targetNode.Dependencies = append(targetNode.Dependencies, currentID)
			}
		}
	}

	return &wf, nil
}

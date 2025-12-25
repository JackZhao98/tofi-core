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

	// 注入 ID (Map Key -> Node.ID)
	for id, node := range wf.Nodes {
		node.ID = id
	}

	return &wf, nil
}

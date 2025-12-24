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

	var wf models.Workflow
	ext := filepath.Ext(path)

	if ext == ".yaml" || ext == ".yml" {
		err = yaml.Unmarshal(data, &wf)
	} else if ext == ".json" {
		err = json.Unmarshal(data, &wf)
	} else {
		return nil, fmt.Errorf("不支持的文件格式: %s", ext)
	}

	if err != nil {
		return nil, err
	}

	return &wf, nil
}

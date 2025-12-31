package toolbox

import (
	"embed"
	"fmt"
)

//go:embed *.yaml
var ToolboxFS embed.FS

// ReadAction 读取指定名称的 action YAML 文件
func ReadAction(actionName string) ([]byte, error) {
	embedPath := actionName + ".yaml"
	data, err := ToolboxFS.ReadFile(embedPath)
	if err != nil {
		return nil, fmt.Errorf("官方 toolbox 组件不存在: %s", actionName)
	}
	return data, nil
}

// Exists 检查指定的组件是否存在
func Exists(actionName string) bool {
	embedPath := actionName + ".yaml"
	_, err := ToolboxFS.ReadFile(embedPath)
	return err == nil
}

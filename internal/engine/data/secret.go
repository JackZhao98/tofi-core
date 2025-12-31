package data

import (
	"fmt"
	"tofi-core/internal/models"
)

type Secret struct{}

func (s *Secret) Execute(config map[string]interface{}, ctx *models.ExecutionContext) (string, error) {
	// 在新规范下，Resolver 已经负责从环境变量获取了 Secret 值
	// 并将其存入了 localContext，随后注入到了 config["value"]
	val := config["value"]
	if val == nil {
		return "", fmt.Errorf("secret value not resolved")
	}

	return fmt.Sprint(val), nil
}

func (s *Secret) Validate(n *models.Node) error {
	return nil
}

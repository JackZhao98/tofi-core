package logic

import (
	"fmt"
	"strings"
	"tofi-core/internal/models"
)

type Check struct{}

func (c *Check) Execute(config map[string]interface{}, ctx *models.ExecutionContext) (string, error) {
	val := fmt.Sprint(config["value"])
	mode := fmt.Sprint(config["mode"])

	var result bool
	switch mode {
	case "is_true":
		result = strings.ToLower(val) == "true" || val == "1"
	case "is_false":
		result = strings.ToLower(val) == "false" || val == "0"
	case "is_empty":
		result = len(strings.TrimSpace(val)) == 0
	case "exists":
		result = len(val) > 0
	default:
		return "", fmt.Errorf("unsupported check mode: %s", mode)
	}

	if !result {
		if strings.ToLower(fmt.Sprint(config["output_bool"])) == "true" {
			return "false", nil
		}
		return "", fmt.Errorf("CONDITION_NOT_MET")
	}

	if strings.ToLower(fmt.Sprint(config["output_bool"])) == "true" {
		return "true", nil
	}
	return "CHECK_PASSED", nil
}

func (c *Check) Validate(n *models.Node) error {
	if _, ok := n.Config["mode"]; !ok {
		return fmt.Errorf("config.mode is required")
	}
	return nil
}

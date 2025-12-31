package logic

import (
	"fmt"
	"regexp"
	"strings"
	"tofi-core/internal/models"
)

type Text struct{}

func (t *Text) Execute(config map[string]interface{}, ctx *models.ExecutionContext) (string, error) {
	target := fmt.Sprint(config["text"])
	pattern := fmt.Sprint(config["pattern"])
	mode := fmt.Sprint(config["mode"])

	var result bool
	switch mode {
	case "contains":
		result = strings.Contains(target, pattern)
	case "not_contains":
		result = !strings.Contains(target, pattern)
	case "starts_with":
		result = strings.HasPrefix(target, pattern)
	case "ends_with":
		result = strings.HasSuffix(target, pattern)
	case "matches":
		re, err := regexp.Compile(pattern)
		if err != nil {
			return "", fmt.Errorf("invalid regex: %v", err)
		}
		result = re.MatchString(target)
	default:
		return "", fmt.Errorf("unsupported text mode: %s", mode)
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
	return "TEXT_MATCHED", nil
}

func (t *Text) Validate(n *models.Node) error {
	if _, ok := n.Config["mode"]; !ok {
		return fmt.Errorf("config.mode is required")
	}
	return nil
}

package logic

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"tofi-core/internal/models"
)

type List struct{}

func (l *List) Execute(config map[string]interface{}, ctx *models.ExecutionContext) (string, error) {
	rawList := config["list"]
	mode := fmt.Sprint(config["mode"])
	targetVal := fmt.Sprint(config["value"])

	var list []interface{}
	switch v := rawList.(type) {
	case []interface{}:
		list = v
	case string:
		if err := json.Unmarshal([]byte(v), &list); err != nil {
			return "", fmt.Errorf("failed to parse list string: %v", err)
		}
	default:
		return "", fmt.Errorf("list input must be an array or JSON string")
	}

	var result bool
	switch mode {
	case "length_equals":
		expectedLen, _ := strconv.Atoi(targetVal)
		result = len(list) == expectedLen
	case "length_gt":
		expectedLen, _ := strconv.Atoi(targetVal)
		result = len(list) > expectedLen
	case "length_lt":
		expectedLen, _ := strconv.Atoi(targetVal)
		result = len(list) < expectedLen
	case "contains":
		for _, item := range list {
			if fmt.Sprint(item) == targetVal {
				result = true
				break
			}
		}
	case "not_contains":
		found := false
		for _, item := range list {
			if fmt.Sprint(item) == targetVal {
				found = true
				break
			}
		}
		result = !found
	default:
		return "", fmt.Errorf("unsupported list mode: %s", mode)
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
	return "LIST_OK", nil
}

func (l *List) Validate(n *models.Node) error {
	if _, ok := n.Config["mode"]; !ok {
		return fmt.Errorf("config.mode is required")
	}
	return nil
}

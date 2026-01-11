package data

import (
	"encoding/json"
	"fmt"
	"strings"
	"tofi-core/internal/models"

	"github.com/tidwall/gjson"
)

type Dict struct{}

func (d *Dict) Execute(config map[string]interface{}, ctx *models.ExecutionContext) (string, error) {
	// 1. 获取可选的输入字符串
	var inputJSON string
	if input, ok := config["input"]; ok && input != nil {
		inputStr := fmt.Sprint(input)
		// 从输入字符串中提取 JSON
		inputJSON = extractJSON(inputStr)
	}

	// 2. 获取用户定义的字段列表
	fieldsRaw, ok := config["fields"]
	if !ok {
		// 没有定义字段，返回空对象
		return "{}", nil
	}

	fields, ok := fieldsRaw.([]interface{})
	if !ok {
		return "{}", nil
	}

	// 3. 构建结果对象
	result := make(map[string]interface{})

	for _, f := range fields {
		fieldMap, ok := f.(map[string]interface{})
		if !ok {
			continue
		}

		key, _ := fieldMap["key"].(string)
		value, _ := fieldMap["value"].(string)

		if key == "" {
			continue
		}

		// 解析 value：
		// - 如果有 inputJSON 且 value 看起来像路径（不包含 {{}}），尝试从 JSON 提取
		// - 如果包含 {{}}，使用模板替换
		// - 否则当作字面值
		resolved := resolveFieldValue(value, inputJSON, ctx)
		result[key] = resolved
	}

	// 4. 返回 JSON 对象
	res, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("dict serialization failed: %v", err)
	}
	return string(res), nil
}

// resolveFieldValue 解析字段值
// value 格式：
// - "input.xxx" -> 从 inputJSON 中提取 xxx 路径
// - "input.xxx.yyy" -> 从 inputJSON 中提取 xxx.yyy 路径
// - "{{input.xxx}}" -> 从 inputJSON 中提取 xxx（模板语法）
// - "this is {{input.key}}" -> 混合模板
// - "{{node_id}}" -> 引用其他节点输出
// - "literal" -> 字面值
func resolveFieldValue(value string, inputJSON string, ctx *models.ExecutionContext) string {
	// 1. 如果是纯 "input.xxx" 格式（无 {{}}），直接提取
	if strings.HasPrefix(value, "input.") && !strings.Contains(value, "{{") {
		if inputJSON == "" {
			return ""
		}
		jsonPath := strings.TrimPrefix(value, "input.")
		if jsonPath == "" {
			return inputJSON
		}
		result := gjson.Get(inputJSON, jsonPath)
		if result.Exists() {
			return result.String()
		}
		return ""
	}

	// 2. 处理模板语法 {{...}}
	if strings.Contains(value, "{{") && strings.Contains(value, "}}") {
		result := value
		// 先替换 {{input.xxx}} 模板
		if inputJSON != "" {
			result = replaceInputTemplates(result, inputJSON)
		}
		// 再用 ctx 替换其他引用 {{node_id}} 等
		result = ctx.ReplaceParams(result)
		return result
	}

	// 3. 字面值
	return value
}

// replaceInputTemplates 替换 {{input.xxx}} 为 inputJSON 中的值
func replaceInputTemplates(value string, inputJSON string) string {
	result := value
	for {
		start := strings.Index(result, "{{input.")
		if start == -1 {
			break
		}
		end := strings.Index(result[start:], "}}")
		if end == -1 {
			break
		}
		end += start

		// 提取 input.xxx 中的 xxx 部分
		fullMatch := result[start : end+2]           // {{input.xxx}}
		innerPath := result[start+2 : end]           // input.xxx
		jsonPath := strings.TrimPrefix(innerPath, "input.")

		// 从 inputJSON 中提取值
		extracted := gjson.Get(inputJSON, jsonPath)
		if extracted.Exists() {
			result = strings.Replace(result, fullMatch, extracted.String(), 1)
		} else {
			// 路径不存在，替换为空
			result = strings.Replace(result, fullMatch, "", 1)
		}
	}
	return result
}

// extractJSON 从字符串中提取第一个 JSON 对象或数组
func extractJSON(s string) string {
	// 查找第一个 { 或 [
	objStart := strings.Index(s, "{")
	arrStart := strings.Index(s, "[")

	var start int
	var openChar, closeChar rune

	if objStart == -1 && arrStart == -1 {
		return ""
	} else if objStart == -1 {
		start = arrStart
		openChar = '['
		closeChar = ']'
	} else if arrStart == -1 {
		start = objStart
		openChar = '{'
		closeChar = '}'
	} else if objStart < arrStart {
		start = objStart
		openChar = '{'
		closeChar = '}'
	} else {
		start = arrStart
		openChar = '['
		closeChar = ']'
	}

	// 找到匹配的结束符
	depth := 0
	inString := false
	escaped := false

	for i := start; i < len(s); i++ {
		c := rune(s[i])

		if escaped {
			escaped = false
			continue
		}

		if c == '\\' && inString {
			escaped = true
			continue
		}

		if c == '"' {
			inString = !inString
			continue
		}

		if inString {
			continue
		}

		if c == openChar {
			depth++
		} else if c == closeChar {
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}

	return ""
}

func (d *Dict) Validate(n *models.Node) error {
	// Dict 节点需要至少定义一个字段
	// 但这个验证可以宽松一些，允许空字段列表
	return nil
}

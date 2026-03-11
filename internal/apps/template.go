package apps

import (
	"encoding/json"
	"regexp"
	"strings"
)

// 模板语法：
// {{param_name}}              — 替换为参数值（无值用 default）
// {{#bool_param}}...{{/bool_param}} — 条件块（true 保留，false 删除）

var (
	// {{param_name}} 简单替换
	paramRegex = regexp.MustCompile(`\{\{([a-zA-Z_][a-zA-Z0-9_]*)\}\}`)

	// {{#bool}}...{{/bool}} 条件块开头标记
	condOpenRegex = regexp.MustCompile(`\{\{#([a-zA-Z_][a-zA-Z0-9_]*)\}\}`)
)

// ResolvePrompt 将 prompt 模板中的 {{param}} 替换为实际值
// paramValues: 用户填入的参数值 {"market": "SP500", "include_crypto": "true"}
// paramDefs:   参数定义（用于获取默认值）
func ResolvePrompt(template string, paramValues map[string]string, paramDefs map[string]*AppParameter) string {
	// 1. 处理条件块 {{#bool_param}}...{{/bool_param}}
	// Go regexp 不支持 backreference，手动查找匹配的 open/close 标签
	result := template
	for {
		loc := condOpenRegex.FindStringSubmatchIndex(result)
		if loc == nil {
			break
		}
		paramName := result[loc[2]:loc[3]]
		closeTag := "{{/" + paramName + "}}"
		closeIdx := strings.Index(result[loc[1]:], closeTag)
		if closeIdx < 0 {
			break // 没有匹配的关闭标签，停止处理
		}
		content := result[loc[1] : loc[1]+closeIdx]
		endIdx := loc[1] + closeIdx + len(closeTag)

		val := resolveValue(paramName, paramValues, paramDefs)
		var replacement string
		if isTruthy(val) {
			replacement = strings.TrimSpace(content)
		}
		result = result[:loc[0]] + replacement + result[endIdx:]
	}

	// 2. 处理简单替换 {{param_name}}
	result = paramRegex.ReplaceAllStringFunc(result, func(match string) string {
		parts := paramRegex.FindStringSubmatch(match)
		if len(parts) < 2 {
			return match
		}
		paramName := parts[1]
		val := resolveValue(paramName, paramValues, paramDefs)
		if val != "" {
			return val
		}
		return match // 未解析的保留原样
	})

	return result
}

// resolveValue 解析参数值：优先用户填入值，其次默认值
func resolveValue(name string, values map[string]string, defs map[string]*AppParameter) string {
	if v, ok := values[name]; ok && v != "" {
		return v
	}
	if defs != nil {
		if def, ok := defs[name]; ok && def.Default != "" {
			return def.Default
		}
	}
	return ""
}

// isTruthy 判断值是否为"真"
func isTruthy(val string) bool {
	val = strings.ToLower(strings.TrimSpace(val))
	return val == "true" || val == "yes" || val == "1"
}

// ResolveFromJSON resolves a prompt template using JSON parameter values and definitions
func ResolveFromJSON(prompt, parametersJSON, parameterDefsJSON string) string {
	if prompt == "" {
		return ""
	}

	var paramValues map[string]string
	if parametersJSON != "" && parametersJSON != "{}" {
		json.Unmarshal([]byte(parametersJSON), &paramValues)
	}

	var paramDefs map[string]*AppParameter
	if parameterDefsJSON != "" && parameterDefsJSON != "{}" {
		json.Unmarshal([]byte(parameterDefsJSON), &paramDefs)
	}

	if len(paramValues) == 0 && len(paramDefs) == 0 {
		return prompt
	}

	return ResolvePrompt(prompt, paramValues, paramDefs)
}

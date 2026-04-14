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

// ResolveFromJSON resolves a prompt template using JSON parameter values and definitions.
// Saved params only — no runtime overrides. For per-run overrides use ResolveWithOverrides.
func ResolveFromJSON(prompt, parametersJSON, parameterDefsJSON string) string {
	return ResolveWithOverrides(prompt, parametersJSON, parameterDefsJSON, nil)
}

// ResolveWithOverrides resolves a prompt template, merging saved parameter values
// with per-run overrides. Precedence (highest first):
//
//	runtime overrides > saved params > paramDef defaults
//
// The runtime map accepts any JSON-compatible scalar (string / number / bool);
// values are stringified before substitution.
func ResolveWithOverrides(
	prompt, parametersJSON, parameterDefsJSON string,
	runtime map[string]interface{},
) string {
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

	if len(runtime) > 0 {
		if paramValues == nil {
			paramValues = make(map[string]string, len(runtime))
		}
		for k, v := range runtime {
			paramValues[k] = stringifyRuntimeValue(v)
		}
	}

	if len(paramValues) == 0 && len(paramDefs) == 0 {
		return prompt
	}

	return ResolvePrompt(prompt, paramValues, paramDefs)
}

func stringifyRuntimeValue(v interface{}) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		s := string(b)
		// Unquote plain JSON strings so downstream substitution sees the raw value
		if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
			var unquoted string
			if json.Unmarshal(b, &unquoted) == nil {
				return unquoted
			}
		}
		return s
	}
}

// MissingRequiredParams returns the names of required parameters that have no
// value from either saved params or runtime overrides. Useful for pre-run
// validation before dispatching.
func MissingRequiredParams(
	parametersJSON, parameterDefsJSON string,
	runtime map[string]interface{},
) []string {
	var paramDefs map[string]*AppParameter
	if parameterDefsJSON != "" && parameterDefsJSON != "{}" {
		json.Unmarshal([]byte(parameterDefsJSON), &paramDefs)
	}
	if len(paramDefs) == 0 {
		return nil
	}

	var saved map[string]string
	if parametersJSON != "" && parametersJSON != "{}" {
		json.Unmarshal([]byte(parametersJSON), &saved)
	}

	var missing []string
	for name, def := range paramDefs {
		if def == nil || !def.Required {
			continue
		}
		if def.Default != "" {
			continue
		}
		if v, ok := saved[name]; ok && v != "" {
			continue
		}
		if v, ok := runtime[name]; ok && stringifyRuntimeValue(v) != "" {
			continue
		}
		missing = append(missing, name)
	}
	return missing
}

package tasks

import (
	"encoding/json"
	"fmt"
	"tofi-core/internal/models"
	"tofi-core/internal/storage"
)

// SkillStore 定义获取 Skill 所需的 DB 接口
// 使用接口避免循环引用
type SkillStore interface {
	GetSkill(id string) (*storage.SkillRecord, error)
}

// Skill 节点类型：执行已安装的 Agent Skill
// 本质是以 SKILL.md 指令为 system prompt 的 AI 节点
type Skill struct{}

func (s *Skill) Execute(config map[string]interface{}, ctx *models.ExecutionContext) (string, error) {
	// 1. 获取 skill_id
	skillID, _ := config["skill_id"].(string)
	if skillID == "" {
		return "", fmt.Errorf("config.skill_id is required")
	}

	// 2. 从 DB 加载 Skill
	db, ok := ctx.DB.(SkillStore)
	if !ok {
		return "", fmt.Errorf("database connection required for skill execution")
	}

	skill, err := db.GetSkill(skillID)
	if err != nil {
		return "", fmt.Errorf("skill '%s' not found: %v", skillID, err)
	}

	// 3. 获取用户输入
	userInput, _ := config["prompt"].(string)
	if userInput == "" {
		userInput, _ = config["user_input"].(string)
	}
	if userInput == "" {
		return "", fmt.Errorf("config.prompt (user input) is required for skill execution")
	}

	// 4. 构建 AI 执行配置
	// 核心思想: SKILL.md body → system prompt, 用户输入 → user prompt
	aiConfig := map[string]interface{}{
		"system": buildSkillSystemPrompt(skill),
		"prompt": userInput,
	}

	// 5. 模型配置 — 用户可覆盖，否则用 Skill 清单中的推荐
	if model, ok := config["model"].(string); ok && model != "" {
		aiConfig["model"] = model
	} else {
		// 从 Skill manifest 中读取推荐模型
		var manifest models.SkillManifest
		if err := json.Unmarshal([]byte(skill.ManifestJSON), &manifest); err == nil && manifest.Model != "" {
			aiConfig["model"] = manifest.Model
		} else {
			// 默认模型
			aiConfig["model"] = "claude-sonnet-4-20250514"
		}
	}

	// 传递其他 AI 配置字段
	for _, key := range []string{"provider", "endpoint", "api_key", "use_system_key", "max_tokens"} {
		if v, ok := config[key]; ok {
			aiConfig[key] = v
		}
	}

	// 6. 如果 Skill 声明了 allowed-tools，配置 MCP
	tools := skill.AllowedToolsList()
	if len(tools) > 0 {
		if mcpServers, ok := config["mcp_servers"]; ok {
			aiConfig["mcp_servers"] = mcpServers
		}
		// TODO: 将 allowed-tools 转换为 MCP 工具配置
		// 这需要在 Phase 3 实现 MCP 集成时完善
	}

	// 7. 复用 AI 节点执行
	ai := &AI{}
	result, err := ai.Execute(aiConfig, ctx)
	if err != nil {
		return "", fmt.Errorf("skill '%s' execution failed: %v", skill.Name, err)
	}

	return result, nil
}

func (s *Skill) Validate(n *models.Node) error {
	skillID, ok := n.Config["skill_id"]
	if !ok || fmt.Sprint(skillID) == "" {
		return fmt.Errorf("config.skill_id is required")
	}
	return nil
}

// buildSkillSystemPrompt 从 Skill 记录构建 system prompt
func buildSkillSystemPrompt(skill *storage.SkillRecord) string {
	// SKILL.md 的 body 就是完整的 AI 指令
	prompt := skill.Instructions

	// 如果有描述，加入上下文前缀
	if skill.Description != "" {
		prompt = fmt.Sprintf("# %s\n\n> %s\n\n%s", skill.Name, skill.Description, prompt)
	}

	return prompt
}

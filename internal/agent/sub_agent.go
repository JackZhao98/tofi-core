package agent

import (
	"context"
	"fmt"

	"tofi-core/internal/models"
	"tofi-core/internal/provider"

	"github.com/google/uuid"
)

// buildSubAgentTool creates the tofi_sub_agent tool that spawns a child agent
// to handle a focused sub-task. The child agent inherits the parent's provider,
// sandbox, skills, and tools, but runs with a fresh conversation.
//
// Only registered for top-level agents (IsSubAgent=false) to prevent recursion.
func buildSubAgentTool(parentCfg AgentConfig) ToolDef {
	return &FuncTool{
		ToolName:        "tofi_sub_agent",
		ToolDisplayName: "Sub-Agent",
		ToolSchema: provider.Tool{
			Name: "tofi_sub_agent",
			Description: "Spawn a sub-agent to handle a focused sub-task independently. " +
				"The sub-agent runs with the same tools and sandbox but a fresh conversation. " +
				"Use this to delegate research, analysis, or any task that can be described in a single prompt. " +
				"Returns the sub-agent's final output as text.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"prompt": map[string]interface{}{
						"type":        "string",
						"description": "The task for the sub-agent to complete. Be specific and self-contained.",
					},
					"model": map[string]interface{}{
						"type":        "string",
						"description": "Optional model override (e.g. 'gpt-5-mini' for cheaper tasks). Default: inherit parent model.",
					},
				},
				"required": []string{"prompt"},
			},
		},
		ExecuteFunc: func(ctx context.Context, args map[string]interface{}) (string, error) {
			prompt, _ := args["prompt"].(string)
			if prompt == "" {
				return "Error: prompt is required", nil
			}

			// Build sub-agent config — inherit from parent, override what's needed
			subCfg := AgentConfig{
				Ctx:        ctx,
				Provider:   parentCfg.Provider,
				Model:      parentCfg.Model,
				System:     "You are a focused sub-agent. Complete the given task thoroughly and return a concise, actionable result. Do not ask questions — work with what you have.",
				Prompt:     prompt,
				SkillTools: parentCfg.SkillTools,
				ExtraTools: parentCfg.ExtraTools,
				SandboxDir: parentCfg.SandboxDir,
				UserDir:    parentCfg.UserDir,
				Executor:   parentCfg.Executor,
				SecretEnv:  parentCfg.SecretEnv,
				Hooks:      parentCfg.Hooks,
				IsSubAgent: true, // prevent recursive spawning
			}

			// Override model if specified
			if m, _ := args["model"].(string); m != "" {
				subCfg.Model = m
			}

			// Create a lightweight execution context for the sub-agent
			subID := "sub-" + uuid.New().String()[:8]
			execCtx := models.NewExecutionContext(subID, "", parentCfg.SandboxDir)

			result, err := RunAgentLoop(subCfg, execCtx)
			if err != nil {
				return fmt.Sprintf("Sub-agent failed: %v", err), nil
			}

			if result.Content == "" {
				return "Sub-agent completed but returned no content.", nil
			}

			return result.Content, nil
		},
	}
}

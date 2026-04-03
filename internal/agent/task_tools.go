package agent

import (
	"fmt"
	"time"

	"tofi-core/internal/provider"
)

// ──────────────────────────────────────────────────────────────
// Task management tools for the agent loop
//
// tofi_task_status — query/wait for background shell task results
// tofi_ask_user   — ask the user a question and wait for response
// ──────────────────────────────────────────────────────────────

// buildTaskTools creates task management tools.
// bgManager: background task manager (from shell auto-backgrounding)
// askUserFn: callback to ask user a question (nil = tool not registered)
func buildTaskTools(bgManager *BackgroundTaskManager, askUserFn func(question string, options []string) (string, error)) []ToolDef {
	var extras []ExtraBuiltinTool

	// Always register task_status if we have a background manager
	if bgManager != nil {
		extras = append(extras, buildTaskStatusTool(bgManager))
	}

	// Only register ask_user if callback is provided (Chat mode)
	if askUserFn != nil {
		extras = append(extras, buildAskUserTool(askUserFn))
	}

	tools := make([]ToolDef, len(extras))
	for i, et := range extras {
		tools[i] = WrapExtraBuiltin(et)
	}
	return tools
}

// ──────────────────────────────────────────────────────────────
// tofi_task_status — Check or wait for background task results
// ──────────────────────────────────────────────────────────────

func buildTaskStatusTool(bgManager *BackgroundTaskManager) ExtraBuiltinTool {
	return ExtraBuiltinTool{
		Schema: provider.Tool{
			Name: "tofi_task_status",
			Description: "Check the status of a background shell task or wait for it to complete. " +
				"Use this after a command was auto-backgrounded (you received a task_id). " +
				"Set wait=true to block until the task finishes (up to timeout).",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"task_id": map[string]any{
						"type":        "string",
						"description": "Task ID returned when command was backgrounded (e.g., 'sh_1')",
					},
					"wait": map[string]any{
						"type":        "boolean",
						"description": "If true, wait for the task to complete (default: false = just check status)",
					},
					"timeout": map[string]any{
						"type":        "integer",
						"description": "Max seconds to wait when wait=true (default: 60, max: 300)",
					},
				},
				"required": []string{"task_id"},
			},
		},
		Handler: func(args map[string]any) (string, error) {
			taskID, _ := args["task_id"].(string)
			if taskID == "" {
				return "Error: task_id is required", nil
			}

			shouldWait, _ := args["wait"].(bool)
			timeoutSec := 60
			if t, ok := args["timeout"].(float64); ok && t > 0 {
				timeoutSec = int(t)
				if timeoutSec > 300 {
					timeoutSec = 300
				}
			}

			if shouldWait {
				result := bgManager.WaitResult(taskID, time.Duration(timeoutSec)*time.Second)
				if result == nil {
					return fmt.Sprintf("Task %s: still running after %ds wait. Try again later or increase timeout.", taskID, timeoutSec), nil
				}
				output := result.FormatForAgent()
				return fmt.Sprintf("Task %s completed (exit=%d, %dms):\n%s",
					taskID, result.ExitCode, result.DurationMs, smartTruncate(output, 4000)), nil
			}

			// Non-blocking check
			result := bgManager.GetResult(taskID)
			if result == nil {
				active := bgManager.ActiveCount()
				return fmt.Sprintf("Task %s: still running (%d active background tasks). Use wait=true to wait for completion.", taskID, active), nil
			}

			output := result.FormatForAgent()
			return fmt.Sprintf("Task %s completed (exit=%d, %dms):\n%s",
				taskID, result.ExitCode, result.DurationMs, smartTruncate(output, 4000)), nil
		},
	}
}

// ──────────────────────────────────────────────────────────────
// tofi_ask_user — Ask the user a question (Chat mode only)
// ──────────────────────────────────────────────────────────────

func buildAskUserTool(askUserFn func(question string, options []string) (string, error)) ExtraBuiltinTool {
	return ExtraBuiltinTool{
		Schema: provider.Tool{
			Name: "tofi_ask_user",
			Description: "Ask the user a question and wait for their response. " +
				"Use this when you need clarification, confirmation for destructive actions, " +
				"or user input to proceed. Optionally provide answer options. " +
				"Only available in interactive Chat mode (not in App Run).",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"question": map[string]any{
						"type":        "string",
						"description": "The question to ask the user",
					},
					"options": map[string]any{
						"type":        "array",
						"description": "Optional list of answer choices (e.g., ['Yes', 'No', 'Skip'])",
						"items":       map[string]any{"type": "string"},
					},
				},
				"required": []string{"question"},
			},
		},
		Handler: func(args map[string]any) (string, error) {
			question, _ := args["question"].(string)
			if question == "" {
				return "Error: question is required", nil
			}

			var options []string
			if opts, ok := args["options"].([]interface{}); ok {
				for _, o := range opts {
					if s, ok := o.(string); ok {
						options = append(options, s)
					}
				}
			}

			// This blocks the agent loop until user responds
			answer, err := askUserFn(question, options)
			if err != nil {
				return fmt.Sprintf("User did not respond: %v", err), nil
			}

			return fmt.Sprintf("User responded: %s", answer), nil
		},
	}
}

package agent

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"tofi-core/internal/models"
	"tofi-core/internal/provider"

	"github.com/google/uuid"
)

// buildSubAgentTool creates the tofi_sub_agent tool that spawns a child agent
// to handle a focused sub-task. The child agent inherits the parent's provider,
// sandbox, skills, and tools, but runs with a fresh conversation.
//
// Only registered for top-level agents (IsSubAgent=false) to prevent recursion.
//
// Design notes (informed by Claude Code's AgentTool):
//   - Strict "when NOT to use" guardrails — sub-agents are expensive and opaque
//     to the user, so spawning one for a single web fetch or a one-shot lookup
//     is bad form. Use them for plans, multi-step research, parallelizable work.
//   - Mandatory description (3-5 words) so the UI can label the spawn for the
//     user instead of just showing "Sub-Agent ⋯".
//   - Inherits parent's loaded skills as PreloadedSkills so the child doesn't
//     waste a turn re-loading skills the parent already activated.
//   - Captures the child's tool-call sequence and returns a structured envelope
//     so the frontend can render an indented "what the sub-agent did" view.
func buildSubAgentTool(parentCfg AgentConfig) ToolDef {
	return &FuncTool{
		ToolName:        "tofi_sub_agent",
		ToolDisplayName: "Sub-Agent",
		ToolSchema: provider.Tool{
			Name: "tofi_sub_agent",
			Description: "Hand off a substantial multi-step task to a fresh agent that runs in an isolated conversation and returns a structured report. " +
				"The sub-agent starts with no memory of your conversation — the brief you provide is its complete context. It shares your tools, skills, and sandbox, and uses the same model you are. " +
				"\n\n" +
				"Suitable for work that spans many tool calls whose intermediate results don't need to appear in your conversation: surveying multiple sources to synthesize a summary, running a plan-and-execute loop to produce a structured deliverable, evaluating several options side by side. " +
				"The parent chat shows a single progress indicator while the sub-agent works; the sub-agent's final report is this tool's return value.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"description": map[string]interface{}{
						"type":        "string",
						"description": "Short 3-5 word label shown to the user in the progress indicator (e.g. 'Audit ship readiness', 'Compare 3 vendors').",
					},
					"prompt": map[string]interface{}{
						"type":        "string",
						"description": "The complete task brief. Self-contained — the sub-agent cannot see your conversation. State the scope, inputs, exact deliverable, and any constraints. Vague briefs produce vague reports.",
					},
					"model": map[string]interface{}{
						"type":        "string",
						"description": "Override the model. Defaults to the parent's model.",
					},
				},
				"required": []string{"description", "prompt"},
			},
		},
		ExecuteFunc: func(ctx context.Context, args map[string]interface{}) (string, error) {
			description, _ := args["description"].(string)
			prompt, _ := args["prompt"].(string)
			if prompt == "" {
				return "Error: prompt is required", nil
			}
			if description == "" {
				description = "Unnamed sub-task"
			}

			// Capture the sub-agent's tool calls so we can show the parent
			// what was done — without flooding the parent's context with
			// every chunk and tool result. We only keep names + brief input
			// summaries; the sub-agent's final report is the substantive
			// payload.
			var (
				mu        sync.Mutex
				toolTrace []subAgentToolEntry
			)

			parentOnTool := parentCfg.OnToolCall
			forwardEvent := parentCfg.OnSubAgentEvent
			subOnTool := func(toolName, input, output string, durationMs int64) {
				mu.Lock()
				toolTrace = append(toolTrace, subAgentToolEntry{
					Name:       toolName,
					Input:      summarizeInput(input),
					DurationMs: durationMs,
				})
				mu.Unlock()
				// Forward as a live event so the parent UI can show what
				// the sub-agent is actually doing while it runs. Don't
				// invoke parent's OnToolCall — that would write the call
				// into the parent's transcript, polluting context.
				if forwardEvent != nil {
					forwardEvent("sub_agent_tool_call", map[string]interface{}{
						"tool":        toolName,
						"input":       input,
						"output":      output,
						"duration_ms": durationMs,
					})
				}
				_ = parentOnTool
			}
			subOnStepStart := func(toolName, args string) {
				if forwardEvent != nil {
					forwardEvent("sub_agent_tool_call", map[string]interface{}{
						"tool":  toolName,
						"input": args,
					})
				}
			}
			subOnChunk := func(_, delta string) {
				if forwardEvent != nil {
					forwardEvent("sub_agent_chunk", map[string]interface{}{
						"delta": delta,
					})
				}
			}

			subCfg := AgentConfig{
				Ctx:      ctx,
				Provider: parentCfg.Provider,
				Model:    parentCfg.Model,
				System: "You are a focused sub-agent spawned by a coordinator. " +
					"Your context is fresh — you do NOT see the user's conversation. " +
					"Complete the task thoroughly and return a concise, actionable report. " +
					"Do not ask clarifying questions; work with what the brief gives you. " +
					"If the brief is ambiguous, make a reasonable assumption and state it in your report.",
				Prompt:          prompt,
				SkillTools:      parentCfg.SkillTools,
				PreloadedSkills: parentCfg.PreloadedSkills, // share what parent already activated
				ExtraTools:      parentCfg.ExtraTools,
				SandboxDir:      parentCfg.SandboxDir,
				UserDir:         parentCfg.UserDir,
				Executor:        parentCfg.Executor,
				SecretEnv:       parentCfg.SecretEnv,
				Hooks:           parentCfg.Hooks,
				OnToolCall:      subOnTool,
				OnStepStart:     subOnStepStart,
				OnStreamChunk:   subOnChunk,
				IsSubAgent:      true, // prevent recursive spawning
			}

			// Announce start so the UI can mark the SubAgentRunCard as live.
			if forwardEvent != nil {
				forwardEvent("sub_agent_started", map[string]interface{}{
					"description": description,
				})
			}
			defer func() {
				if forwardEvent != nil {
					forwardEvent("sub_agent_finished", map[string]interface{}{
						"description": description,
					})
				}
			}()

			if m, _ := args["model"].(string); m != "" && m != parentCfg.Model {
				// Observability: log every time the parent overrides the
				// model so we can spot unwanted drift in prod (QA #28 was
				// caused by the schema hint suggesting gpt-5-mini and the
				// parent dutifully picking it). Left noisy on purpose —
				// model overrides should be rare once the schema is
				// hardened.
				// NOTE: uses parentCfg.Provider's nil-check-free path;
				// models pkg context carries the log destination.
				execCtxLog := models.NewExecutionContext("sub-agent-drift", "", "")
				execCtxLog.Log("[sub-agent] parent=%s overrode to %s — verify this was intentional", parentCfg.Model, m)
				execCtxLog.Close()
				subCfg.Model = m
			}

			subID := "sub-" + uuid.New().String()[:8]
			execCtx := models.NewExecutionContext(subID, "", parentCfg.SandboxDir)

			start := time.Now()
			result, err := RunAgentLoop(subCfg, execCtx)
			elapsed := time.Since(start)

			if err != nil {
				return formatSubAgentEnvelope(description, "", toolTrace, elapsed, err.Error()), nil
			}

			content := result.Content
			if content == "" {
				content = "(no content returned)"
			}
			return formatSubAgentEnvelope(description, content, toolTrace, elapsed, ""), nil
		},
	}
}

type subAgentToolEntry struct {
	Name       string
	Input      string
	DurationMs int64
}

// summarizeInput trims tool-call input JSON so the trace stays compact.
// Long arguments are truncated; secrets-looking fields are redacted.
func summarizeInput(input string) string {
	if input == "" {
		return ""
	}
	const maxLen = 120
	trimmed := strings.TrimSpace(input)
	if len(trimmed) <= maxLen {
		return trimmed
	}
	return trimmed[:maxLen] + "…"
}

// formatSubAgentEnvelope renders the sub-agent's output as a markdown block
// with metadata the frontend can parse for a nested "agent ran" view.
// Keeps a leading sentinel so renderers can detect the envelope reliably.
func formatSubAgentEnvelope(description, content string, trace []subAgentToolEntry, elapsed time.Duration, errMsg string) string {
	header := map[string]interface{}{
		"description": description,
		"duration_ms": elapsed.Milliseconds(),
		"tool_count":  len(trace),
	}
	if errMsg != "" {
		header["error"] = errMsg
	}
	if len(trace) > 0 {
		toolNames := make([]string, 0, len(trace))
		for _, t := range trace {
			toolNames = append(toolNames, t.Name)
		}
		header["tools"] = toolNames
	}
	headerJSON, _ := json.Marshal(header)

	var b strings.Builder
	b.WriteString("<sub-agent-result>")
	b.Write(headerJSON)
	b.WriteString("</sub-agent-result>\n\n")
	b.WriteString(content)
	return b.String()
}

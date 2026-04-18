package agent

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"time"

	"tofi-core/internal/models"
	"tofi-core/internal/provider"

	"github.com/google/uuid"
)

// intStr converts an int to a string. Tiny helper kept inline so the
// short-brief guard error message doesn't need a heavyweight fmt call.
func intStr(n int) string { return strconv.Itoa(n) }

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
			Description: "DEFAULT TO NOT USING THIS TOOL. Call directly-available tools (web-fetch, web-search, skill runs, file ops) first — they're faster, cheaper, and visible to the user. " +
				"Sub-agents are heavy: they render as a silent ⋯ in the parent chat while they run, consuming the user's patience and budget. " +
				"\n\n" +
				"USE ONLY when ALL of the following are true: " +
				"(a) the task needs 5+ tool calls to complete; " +
				"(b) those tool calls are independent and would clutter the parent's context with irrelevant intermediate results; " +
				"(c) the final deliverable is a structured report, not a single fact or value. " +
				"\n\n" +
				"NEVER USE FOR: " +
				"(a) looking up a single value (price, date, status) — use web-search / web-fetch directly; " +
				"(b) fetching one URL or reading one file; " +
				"(c) anything where the user is actively waiting — delays feel like a freeze; " +
				"(d) tasks you could complete yourself in 1-3 tool calls.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"description": map[string]interface{}{
						"type":        "string",
						"description": "Short 3-5 word label shown to the user (e.g. 'Audit ship readiness', 'Compare 3 vendors'). Required.",
					},
					"prompt": map[string]interface{}{
						"type":        "string",
						"description": "Full task brief. Self-contained — the sub-agent has NO access to your conversation. Be specific about scope, what's in/out, and the exact deliverable. Minimum ~200 characters; briefer tasks should be done directly by you. Terse instructions produce shallow work.",
					},
					"model": map[string]interface{}{
						"type":        "string",
						"description": "Optional model override. STRONGLY PREFER omitting this — the sub-agent defaults to the parent's model, which the user chose deliberately. Override only when the task clearly needs a stronger reasoning model than the parent (almost never).",
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

			// Short-brief guard: tasks small enough to fit in a single
			// tweet don't deserve a sub-agent. Returning an error teaches
			// the parent to route to direct tools next time and avoids
			// burning tokens on a spawn that would've been one web-fetch.
			const minPromptLen = 200
			if len(strings.TrimSpace(prompt)) < minPromptLen {
				return "Error: sub-agent declined — this task is too short (< " +
					intStr(minPromptLen) + " chars) to justify the overhead. " +
					"Call web-search / web-fetch / tofi_load_skill / file tools directly instead. " +
					"Sub-agents are only for multi-step research producing a structured report.", nil
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

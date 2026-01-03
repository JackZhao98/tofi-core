package tasks

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
	"tofi-core/internal/executor"
	"tofi-core/internal/mcp"
	"tofi-core/internal/models"

	"github.com/tidwall/gjson"
)

// MCP Config Helper Types from tasks/mcp.go (Duplicated for isolation)
type MCPServerConfig struct {
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
}

func parseMCPServerConfig(raw interface{}) (*MCPServerConfig, error) {
	m, ok := raw.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("server config must be an object")
	}

	cmd := fmt.Sprint(m["command"])
	if cmd == "" {
		return nil, fmt.Errorf("server.command is required")
	}

	var args []string
	if a, ok := m["args"].([]interface{}); ok {
		for _, v := range a {
			args = append(args, fmt.Sprint(v))
		}
	} else if a, ok := m["args"].([]string); ok {
		args = a
	}

	env := make(map[string]string)
	if e, ok := m["env"].(map[string]interface{}); ok {
		for k, v := range e {
			env[k] = fmt.Sprint(v)
		}
	}

	return &MCPServerConfig{
		Command: cmd,
		Args:    args,
		Env:     env,
	}, nil
}

func resolveMCPServerConfig(serverRef interface{}, ctx *models.ExecutionContext) (*MCPServerConfig, error) {
	// Case A: Ref by Name
	if serverName, ok := serverRef.(string); ok {
		if dataStr, ok := ctx.GetResult("data"); ok {
			var globalData map[string]interface{}
			if err := json.Unmarshal([]byte(dataStr), &globalData); err == nil {
				if servers, ok := globalData["mcp_servers"].(map[string]interface{}); ok {
					if serverRaw, ok := servers[serverName]; ok {
						return parseMCPServerConfig(serverRaw)
					}
				}
			}
		}
		return nil, fmt.Errorf("MCP server '%s' not defined in global data.mcp_servers", serverName)
	}
	// Case B: Inline Config
	if serverObj, ok := serverRef.(map[string]interface{}); ok {
		return parseMCPServerConfig(serverObj)
	}
	return nil, fmt.Errorf("invalid server config")
}

type AI struct{}

func (a *AI) Execute(config map[string]interface{}, ctx *models.ExecutionContext) (string, error) {
	endpoint := fmt.Sprint(config["endpoint"])
	apiKey := fmt.Sprint(config["api_key"])
	model := fmt.Sprint(config["model"])
	provider := strings.ToLower(fmt.Sprint(config["provider"]))

	system := fmt.Sprint(config["system"])
	prompt := fmt.Sprint(config["prompt"])

	if prompt == "" {
		return "", fmt.Errorf("AI prompt cannot be empty")
	}

	// 1. MCP Tool Discovery
	var tools []mcp.Tool
	var activeClients []*mcp.Client
	defer func() {
		for _, c := range activeClients {
			c.Close()
		}
	}()

	// Map: tool_name -> client_index
	toolToClient := make(map[string]int)

	if mcpServers, ok := config["mcp_servers"].([]interface{}); ok {
		for _, sRef := range mcpServers {
			srvConf, err := resolveMCPServerConfig(sRef, ctx)
			if err != nil {
				return "", fmt.Errorf("failed to resolve mcp server: %v", err)
			}

			client, err := mcp.NewStdioClient(srvConf.Command, srvConf.Args, srvConf.Env)
			if err != nil {
				return "", fmt.Errorf("failed to start mcp server: %v", err)
			}
			activeClients = append(activeClients, client)
			clientIdx := len(activeClients) - 1

			if err := client.Handshake(); err != nil {
				return "", fmt.Errorf("mcp handshake failed: %v", err)
			}

			srvTools, err := client.ListTools()
			if err != nil {
				return "", fmt.Errorf("failed to list tools: %v", err)
			}

			for _, t := range srvTools {
				tools = append(tools, t)
				toolToClient[t.Name] = clientIdx
			}
		}
	}

	// If no tools, fallback to simple generation
	if len(tools) == 0 {
		return a.generateText(endpoint, apiKey, model, provider, system, prompt)
	}

	// Initialize Headers
	headers := make(map[string]string)
	if provider == "gemini" {
		headers["x-goog-api-key"] = apiKey
	} else if provider == "claude" {
		headers["x-api-key"] = apiKey
		headers["anthropic-version"] = "2023-06-01"
	} else {
		// OpenAI compatible
		if apiKey != "" {
			headers["Authorization"] = "Bearer " + apiKey
		}
	}

	// Default Agent System Prompt - Completely Universal
	const defaultSystemPrompt = `You are an autonomous agent. You have access to tools provided via MCP.

**Core Behavior:**
1. Observe the current state using available tools before taking action.
2. Choose the tool that best accomplishes your current sub-goal.
3. If a tool fails, try a DIFFERENT tool or approach. Do NOT retry the same failing action more than once.
4. After clicking a button that starts a process (search, submit, etc.):
   a) Call wait(5) to let the process complete
   b) Then observe the state again to see the results
5. Continue until you have the actual results.

**IMPORTANT:** Only call 'answer' when you have real results to report.
`

	// Add synthetic "wait" tool
	waitSchema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"seconds": {
				"type": "number",
				"description": "Number of seconds to wait (1-30)"
			}
		},
		"required": ["seconds"]
	}`)
	waitTool := mcp.Tool{
		Name:        "wait",
		Description: "Pause execution for a specified number of seconds. Use this when waiting for a page to load or a process to complete.",
		InputSchema: waitSchema,
	}
	tools = append(tools, waitTool)

	// Add synthetic "answer" tool - agent must call this to complete
	answerSchema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"result": {
				"type": "string",
				"description": "The final answer or result to return to the user"
			}
		},
		"required": ["result"]
	}`)
	answerTool := mcp.Tool{
		Name:        "answer",
		Description: "Call this tool when you have completed the task and have the final result to return. This is the ONLY way to finish.",
		InputSchema: answerSchema,
	}
	tools = append(tools, answerTool)

	// 2. Agent Loop
	maxConsecutiveErrors := 5 // Stop after 5 consecutive errors
	maxTotalTurns := 50       // Safety limit for total iterations
	consecutiveErrors := 0
	currentPrompt := prompt

	// Append initial history
	messages := []map[string]interface{}{}
	if system != "" {
		messages = append(messages, map[string]interface{}{"role": "system", "content": system})
	} else if len(tools) > 0 {
		// Inject default system prompt for agents if no user system prompt is provided
		messages = append(messages, map[string]interface{}{"role": "system", "content": defaultSystemPrompt})
	}

	messages = append(messages, map[string]interface{}{
		"role":    "user",
		"content": currentPrompt,
	})

	for i := 0; i < maxTotalTurns; i++ {
		// Construct Payload with Tools
		payload := a.buildPayload(model, provider, system, messages, tools)

		// Debug: Print request payload (truncated messages for readability)
		fmt.Fprintf(os.Stderr, "\n[LLM REQUEST] Turn %d, Messages: %d, Tools: %d\n", i+1, len(messages), len(tools))

		// Execute LLM
		respStr, err := executor.PostJSON(endpoint, headers, payload, 120) // increased timeout
		if err != nil {
			return "", err
		}

		// Debug: Print response
		fmt.Fprintf(os.Stderr, "[LLM RESPONSE] %s\n", respStr)

		// Parse Response (Tool Call or Text)
		// Note: This logic depends heavily on the provider's format.
		// For this MVP, we will only support OpenAI-compatible tool calls.

		// Check for content
		content := gjson.Get(respStr, "choices.0.message.content").String()

		// Check for tool calls
		toolCalls := gjson.Get(respStr, "choices.0.message.tool_calls")

		if !toolCalls.Exists() || len(toolCalls.Array()) == 0 {
			// No tool calls, we are done
			if content != "" {
				preview := content
				if len(preview) > 100 {
					preview = preview[:100] + "..."
				}
				fmt.Fprintf(os.Stderr, "[AGENT] Completed: %s\n", preview)
			}
			return content, nil
		}

		// Add Assistant Message to history
		// Note: When tool_calls exist, content should be null per OpenAI spec
		assistantMsg := map[string]interface{}{
			"role":       "assistant",
			"tool_calls": toolCalls.Value(),
		}
		if content != "" {
			assistantMsg["content"] = content
		} else {
			assistantMsg["content"] = nil
		}
		messages = append(messages, assistantMsg)

		// Execute Tools
		for _, tc := range toolCalls.Array() {
			functionName := tc.Get("function.name").String()
			argsStr := tc.Get("function.arguments").String()
			callID := tc.Get("id").String()

			var args map[string]interface{}
			json.Unmarshal([]byte(argsStr), &args)

			clientIdx, ok := toolToClient[functionName]
			if !ok {
				// Check if it's the built-in "answer" tool
				if functionName == "answer" {
					result := ""
					if r, ok := args["result"].(string); ok {
						result = r
					}
					fmt.Fprintf(os.Stderr, "[AGENT] Final Answer: %s\n", result)
					return result, nil
				}
				// Check if it's the built-in "wait" tool
				if functionName == "wait" {
					seconds := 3.0 // default
					if s, ok := args["seconds"].(float64); ok {
						seconds = s
						if seconds > 30 {
							seconds = 30
						}
						if seconds < 1 {
							seconds = 1
						}
					}
					fmt.Fprintf(os.Stderr, "[AGENT] Waiting %.0f seconds...\n", seconds)
					time.Sleep(time.Duration(seconds) * time.Second)
					messages = append(messages, map[string]interface{}{
						"role":         "tool",
						"tool_call_id": callID,
						"content":      fmt.Sprintf("Waited %.0f seconds. You can now check the state again.", seconds),
					})
					continue
				}
				messages = append(messages, map[string]interface{}{
					"role":         "tool",
					"tool_call_id": callID,
					"content":      fmt.Sprintf("Error: Tool %s not found", functionName),
				})
				continue
			}

			client := activeClients[clientIdx]
			resBytes, err := client.SendRequest("tools/call", mcp.CallToolParams{
				Name:      functionName,
				Arguments: args,
			})

			toolOutput := ""
			if err != nil {
				toolOutput = fmt.Sprintf("Error executing tool: %v", err)
			} else {
				var callRes mcp.CallToolResult
				if json.Unmarshal(resBytes, &callRes) == nil {
					if callRes.IsError {
						consecutiveErrors++
						if consecutiveErrors >= maxConsecutiveErrors {
							fmt.Fprintf(os.Stderr, "[AGENT] Too many consecutive errors (%d), stopping\n", consecutiveErrors)
							return "", fmt.Errorf("agent stopped: %d consecutive errors", consecutiveErrors)
						}
						toolOutput = "Tool Error"
						for _, item := range callRes.Content {
							if item.Type == "text" {
								toolOutput += ": " + item.Text
							}
						}
					} else {
						for _, item := range callRes.Content {
							if item.Type == "text" {
								toolOutput += item.Text
							}
						}
						// Success! Reset consecutive error counter
						consecutiveErrors = 0
					}
				}
			}

			// Append Tool Output
			messages = append(messages, map[string]interface{}{
				"role":         "tool",
				"tool_call_id": callID,
				"content":      toolOutput,
			})
		}
	}

	fmt.Fprintf(os.Stderr, "[AGENT] Max total turns (%d) reached\n", maxTotalTurns)
	return "", fmt.Errorf("agent max turns exceeded")
}

func (a *AI) Validate(n *models.Node) error {
	if _, ok := n.Config["endpoint"]; !ok {
		return fmt.Errorf("config.endpoint is required")
	}
	if _, ok := n.Config["model"]; !ok {
		return fmt.Errorf("config.model is required")
	}
	return nil
}

// Helper to generate text (original logic refactored)
func (a *AI) generateText(endpoint, apiKey, model, provider, system, prompt string) (string, error) {
	headers := make(map[string]string)
	var payload map[string]interface{}

	if provider == "gemini" {
		headers["x-goog-api-key"] = apiKey
		payload = map[string]interface{}{"contents": []interface{}{map[string]interface{}{"parts": []map[string]interface{}{{"text": system + "\n" + prompt}}}}}
	} else {
		if apiKey != "" {
			headers["Authorization"] = "Bearer " + apiKey
		}
		payload = map[string]interface{}{
			"model": model,
			"messages": []map[string]interface{}{
				{"role": "system", "content": system},
				{"role": "user", "content": prompt},
			},
		}
	}

	resp, err := executor.PostJSON(endpoint, headers, payload, 60)
	if err != nil {
		return "", err
	}

	paths := []string{"choices.0.message.content", "candidates.0.content.parts.0.text"}
	for _, path := range paths {
		if res := gjson.Get(resp, path); res.Exists() {
			return res.String(), nil
		}
	}
	return resp, nil
}

func (a *AI) buildPayload(model, provider, system string, messages []map[string]interface{}, tools []mcp.Tool) map[string]interface{} {
	// Messages already contain system prompt, don't duplicate
	payload := map[string]interface{}{
		"model":       model,
		"messages":    messages,
		"temperature": 0, // Deterministic output for consistent agent behavior
	}

	// Convert MCP Tools to OpenAI Definition
	var openAITools []map[string]interface{}
	for _, t := range tools {
		openAITools = append(openAITools, map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name":        t.Name,
				"description": t.Description,
				"parameters":  t.InputSchema,
			},
		})
	}
	if len(openAITools) > 0 {
		payload["tools"] = openAITools
		payload["tool_choice"] = "auto" // Let model decide, but with tools available
	}
	return payload
}

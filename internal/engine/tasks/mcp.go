package tasks

import (
	"encoding/json"
	"fmt"
	"tofi-core/internal/mcp"
	"tofi-core/internal/models"
)

type MCP struct{}

// ServerConfig 定义单个 MCP 服务器的配置
type ServerConfig struct {
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
}

func (m *MCP) Execute(config map[string]interface{}, ctx *models.ExecutionContext) (string, error) {
	// 1. Resolve configuration
	serverConf, err := m.resolveServerConfig(config, ctx)
	if err != nil {
		return "", err
	}

	method := fmt.Sprint(config["method"])
	if method == "" || method == "<nil>" {
		method = "tools/call" // default
	}

	// 2. Prepare params
	var params map[string]interface{}
	if p, ok := config["params"].(map[string]interface{}); ok {
		params = p
	} else {
		params = make(map[string]interface{})
	}

	// 3. Connect to Server
	client, err := mcp.NewStdioClient(serverConf.Command, serverConf.Args, serverConf.Env)
	if err != nil {
		return "", fmt.Errorf("failed to start MCP server: %v", err)
	}
	defer client.Close()

	if err := client.Handshake(); err != nil {
		return "", fmt.Errorf("MCP handshake failed: %v", err)
	}

	// 4. Perform Action
	switch method {
	case "tools/call":
		toolName := fmt.Sprint(params["name"])
		toolArgs := make(map[string]interface{})
		if a, ok := params["arguments"].(map[string]interface{}); ok {
			toolArgs = a
		} else if a, ok := params["args"].(map[string]interface{}); ok {
			// Compatibility with some user habits
			toolArgs = a
		}

		resBytes, err := client.SendRequest("tools/call", mcp.CallToolParams{
			Name:      toolName,
			Arguments: toolArgs,
		})
		if err != nil {
			return "", err
		}

		// Parse result to find "text" content
		var callRes mcp.CallToolResult
		if err := json.Unmarshal(resBytes, &callRes); err != nil {
			return "", fmt.Errorf("invalid tools/call response: %v", err)
		}

		if callRes.IsError {
			errMsg := "tool execution error"
			for _, item := range callRes.Content {
				if item.Type == "text" {
					errMsg += ": " + item.Text
				}
			}
			return "", fmt.Errorf(errMsg)
		}

		// Concatenate all text parts
		output := ""
		for _, item := range callRes.Content {
			if item.Type == "text" {
				output += item.Text
			} else if item.Type == "resource" && item.Resource != nil {
				output += item.Resource.Text // Basic support for embedded resources
			}
		}
		return output, nil

	case "resources/read":
		uri := fmt.Sprint(params["uri"])
		resBytes, err := client.SendRequest("resources/read", map[string]string{"uri": uri})
		if err != nil {
			return "", err
		}

		// Parse resource result
		// Standard response: { "contents": [ { "uri": "...", "mimeType": "...", "text": "..." } ] }
		type ReadResourceResult struct {
			Contents []mcp.ResourceContents `json:"contents"`
		}
		var readRes ReadResourceResult
		if err := json.Unmarshal(resBytes, &readRes); err != nil {
			return "", fmt.Errorf("invalid resources/read response: %v", err)
		}
		if len(readRes.Contents) > 0 {
			return readRes.Contents[0].Text, nil
		}
		return "", nil

	default:
		return "", fmt.Errorf("unsupported MCP method: %s", method)
	}
}

func (m *MCP) resolveServerConfig(config map[string]interface{}, ctx *models.ExecutionContext) (*ServerConfig, error) {
	// Case A: Ref by Name (Workflow Registry or External Config)
	if serverName, ok := config["server"].(string); ok {
		// Try to lookup from Global Data "mcp_servers"
		if dataStr, ok := ctx.GetResult("data"); ok {
			var globalData map[string]interface{}
			if err := json.Unmarshal([]byte(dataStr), &globalData); err == nil {
				if servers, ok := globalData["mcp_servers"].(map[string]interface{}); ok {
					if serverRaw, ok := servers[serverName]; ok {
						return parseServerConfig(serverRaw)
					}
				}
			}
		}
		return nil, fmt.Errorf("MCP server '%s' not defined in global data.mcp_servers", serverName)
	}

	// Case B: Inline Config
	if serverObj, ok := config["server"].(map[string]interface{}); ok {
		return parseServerConfig(serverObj)
	}

	return nil, fmt.Errorf("invalid 'server' config: must be a name (string) or object settings")
}

func parseServerConfig(raw interface{}) (*ServerConfig, error) {
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
	} else if a, ok := m["args"].([]string); ok { // unlikely from json unmarshal but possible
		args = a
	}

	env := make(map[string]string)
	if e, ok := m["env"].(map[string]interface{}); ok {
		for k, v := range e {
			env[k] = fmt.Sprint(v)
		}
	}

	return &ServerConfig{
		Command: cmd,
		Args:    args,
		Env:     env,
	}, nil
}

func (m *MCP) Validate(n *models.Node) error {
	if _, ok := n.Config["server"]; !ok {
		return fmt.Errorf("config.server is required")
	}
	return nil
}

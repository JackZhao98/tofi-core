package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

// JSON-RPC 2.0 Types

type JSONRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
	ID      interface{} `json:"id,omitempty"`
}

type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
	ID      interface{}     `json:"id"`
}

type JSONRPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

type JSONRPCNotification struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// MCP Protocol Types

type InitializeParams struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ClientCapabilities `json:"capabilities"`
	ClientInfo      ClientInfo         `json:"clientInfo"`
}

type ClientCapabilities struct {
	Roots *struct {
		ListChanged bool `json:"listChanged,omitempty"`
	} `json:"roots,omitempty"`
	Sampling *struct{} `json:"sampling,omitempty"`
}

type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type InitializeResult struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ServerCapabilities `json:"capabilities"`
	ServerInfo      ServerInfo         `json:"serverInfo"`
}

type ServerCapabilities struct {
	Tools     *struct{} `json:"tools,omitempty"`
	Resources *struct{} `json:"resources,omitempty"`
	Prompts   *struct{} `json:"prompts,omitempty"`
}

type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// CallToolParams handles tools/call
// CallToolParams handles tools/call
type CallToolParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"` // Remove omitempty to force {}
}

// CallToolResult handles tools/call response
type CallToolResult struct {
	Content []ContentItem `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

type ContentItem struct {
	Type     string            `json:"type"` // "text" or "image" or "resource"
	Text     string            `json:"text,omitempty"`
	Data     string            `json:"data,omitempty"`     // for image/resource (base64)
	MimeType string            `json:"mimeType,omitempty"` // for image/resource
	Resource *ResourceContents `json:"resource,omitempty"` // embedded resource
}

type ResourceContents struct {
	Uri      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
	Blob     string `json:"blob,omitempty"`
}

// Tool Definition Types

type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema"` // JSON Schema
}

type ListToolsResult struct {
	Tools []Tool `json:"tools"`
}

// Client represents a connection to an MCP server
type Client struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser // Optional debug logging

	// Internal state
	scanner *bufio.Scanner
	mutex   sync.Mutex
	pending map[string]chan JSONRPCResponse // ID -> Response channel
	nextID  int
	closing bool
}

// NewStdioClient creates a client that runs a local command
func NewStdioClient(command string, args []string, env map[string]string) (*Client, error) {
	cmd := exec.Command(command, args...)

	// Set Environment
	if len(env) > 0 {
		cmd.Env = append(exec.Command("true").Environ(), "PYTHONUNBUFFERED=1") // Base env
		for k, v := range env {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
		}
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	c := &Client{
		cmd:     cmd,
		stdin:   stdin,
		stdout:  stdout,
		stderr:  stderr,
		pending: make(map[string]chan JSONRPCResponse),
		nextID:  1,
	}

	// Start reading stdout in a background goroutine
	c.scanner = bufio.NewScanner(stdout)
	// Increase buffer size to handle large tool outputs (e.g. 1MB+)
	buf := make([]byte, 0, 64*1024)
	c.scanner.Buffer(buf, 10*1024*1024)

	go c.listen()
	// Optional: Log stderr in background
	go func() {
		reader := bufio.NewReader(stderr)
		for {
			line, _, err := reader.ReadLine()
			if err != nil {
				return
			}
			_ = line // Can be hooked into logger later
		}
	}()

	return c, nil
}

func (c *Client) listen() {
	for c.scanner.Scan() {
		line := c.scanner.Bytes()
		// Simplified RX log will be handled in SendRequest after parsing

		var resp JSONRPCResponse
		// Try parsing as Response first
		if err := json.Unmarshal(line, &resp); err == nil && (resp.ID != nil || resp.Error != nil) {
			// It's a response matching an ID
			idStr := fmt.Sprintf("%v", resp.ID)

			c.mutex.Lock()
			ch, ok := c.pending[idStr]
			if ok {
				delete(c.pending, idStr)
			}
			c.mutex.Unlock()

			if ok {
				ch <- resp
			}
		} else {
			// Might be a Notification or malformed
			// For now, ignore notifications
		}
	}
}

// SendRequest sends a request and waits for a response
func (c *Client) SendRequest(method string, params interface{}) (json.RawMessage, error) {
	c.mutex.Lock()
	id := c.nextID
	c.nextID++
	idStr := fmt.Sprintf("%d", id)
	ch := make(chan JSONRPCResponse, 1)
	c.pending[idStr] = ch
	c.mutex.Unlock()

	req := JSONRPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
		ID:      id,
	}

	bytes, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	// Write line + newline
	payload := append(bytes, '\n')
	// Simplified TX log
	if method == "tools/call" {
		if p, ok := params.(CallToolParams); ok {
			argsJSON, _ := json.Marshal(p.Arguments)
			fmt.Fprintf(os.Stderr, "[MCP] -> %s(%s)\n", p.Name, string(argsJSON))
		}
	} else {
		fmt.Fprintf(os.Stderr, "[MCP] -> %s\n", method)
	}

	if _, err := c.stdin.Write(payload); err != nil {
		return nil, err
	}

	select {
	case resp := <-ch:
		// Simplified RX log
		if resp.Error != nil {
			fmt.Fprintf(os.Stderr, "[MCP] <- ERROR: %s\n", resp.Error.Message)
			return nil, fmt.Errorf("RPC Error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		// Parse result for brief summary
		var callRes CallToolResult
		if json.Unmarshal(resp.Result, &callRes) == nil && len(callRes.Content) > 0 {
			text := callRes.Content[0].Text
			if len(text) > 80 {
				text = text[:80] + "..."
			}
			if callRes.IsError {
				fmt.Fprintf(os.Stderr, "[MCP] <- ERROR: %s\n", text)
			} else {
				fmt.Fprintf(os.Stderr, "[MCP] <- OK: %s\n", text)
			}
		} else {
			fmt.Fprintf(os.Stderr, "[MCP] <- OK\n")
		}
		return resp.Result, nil
	case <-time.After(60 * time.Second): // Default timeout
		return nil, fmt.Errorf("request timeout")
	}
}

// Handshake performs the initialize sequence
func (c *Client) Handshake() error {
	initParams := InitializeParams{
		ProtocolVersion: "2024-11-05", // LATEST
		Capabilities: ClientCapabilities{
			Roots: &struct {
				ListChanged bool `json:"listChanged,omitempty"`
			}{ListChanged: false},
		},
		ClientInfo: ClientInfo{Name: "tofi-engine", Version: "1.0.0"},
	}

	resBytes, err := c.SendRequest("initialize", initParams)
	if err != nil {
		return fmt.Errorf("initialize failed: %v", err)
	}

	var res InitializeResult
	if err := json.Unmarshal(resBytes, &res); err != nil {
		return fmt.Errorf("failed to parse initialize result: %v", err)
	}

	// Check version compatibility if needed
	// ...

	// Send initialized notification
	notif := JSONRPCNotification{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	}
	notifBytes, _ := json.Marshal(notif)
	c.stdin.Write(append(notifBytes, '\n'))

	return nil
}

// ListTools calls tools/list
func (c *Client) ListTools() ([]Tool, error) {
	resBytes, err := c.SendRequest("tools/list", map[string]interface{}{})
	if err != nil {
		return nil, err
	}

	var res ListToolsResult
	if err := json.Unmarshal(resBytes, &res); err != nil {
		return nil, fmt.Errorf("failed to parse tools/list result: %v", err)
	}
	return res.Tools, nil
}

// Close terminates the process
func (c *Client) Close() error {
	c.closing = true
	c.stdin.Close()
	// Kill the process instead of waiting (it may be blocked)
	if c.cmd.Process != nil {
		c.cmd.Process.Kill()
	}
	return c.cmd.Wait()
}

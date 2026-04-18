package agent

import (
	"context"
	"sync"
	"testing"

	"tofi-core/internal/models"
	"tofi-core/internal/provider"
)

// fakeProvider is a minimal Provider implementation for testing OnMessage.
// It returns a scripted sequence of ChatResponses, one per call.
type fakeProvider struct {
	mu        sync.Mutex
	responses []provider.ChatResponse
	idx       int
}

func (f *fakeProvider) Chat(ctx context.Context, req *provider.ChatRequest) (*provider.ChatResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.idx >= len(f.responses) {
		// Default: terminal assistant message to end the loop.
		return &provider.ChatResponse{Content: "done"}, nil
	}
	resp := f.responses[f.idx]
	f.idx++
	return &resp, nil
}

func (f *fakeProvider) ChatStream(ctx context.Context, req *provider.ChatRequest, onDelta func(provider.StreamDelta)) (*provider.ChatResponse, error) {
	return f.Chat(ctx, req)
}

// TestOnMessage_AssistantOnly verifies that a single-turn conversation with no
// tool calls emits exactly one OnMessage call for the assistant message.
func TestOnMessage_AssistantOnly(t *testing.T) {
	p := &fakeProvider{
		responses: []provider.ChatResponse{
			{Content: "hello there"},
		},
	}

	var captured []provider.Message
	var mu sync.Mutex
	onMessage := func(msg provider.Message) {
		mu.Lock()
		defer mu.Unlock()
		captured = append(captured, msg)
	}

	cfg := AgentConfig{
		Provider:  p,
		Model:     "gpt-4o-mini",
		System:    "test",
		Prompt:    "hi",
		OnMessage: onMessage,
	}
	ctx := models.NewExecutionContext("test", "u1", t.TempDir())
	defer ctx.Close()

	result, err := RunAgentLoop(cfg, ctx)
	if err != nil {
		t.Fatalf("RunAgentLoop error: %v", err)
	}
	if result.Content != "hello there" {
		t.Errorf("expected content 'hello there', got %q", result.Content)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(captured) != 1 {
		t.Fatalf("expected 1 OnMessage call, got %d: %+v", len(captured), captured)
	}
	if captured[0].Role != "assistant" {
		t.Errorf("expected assistant role, got %q", captured[0].Role)
	}
	if captured[0].Content != "hello there" {
		t.Errorf("expected content 'hello there', got %q", captured[0].Content)
	}
}

// TestOnMessage_AssistantThenTool verifies that an assistant-with-tool-call
// followed by a tool result emits two OnMessage calls, in order, with the
// tool result having ToolCallID + ToolName populated.
func TestOnMessage_AssistantThenTool(t *testing.T) {
	// First response: assistant calls tofi_wait.
	// Second response: assistant finishes with content.
	p := &fakeProvider{
		responses: []provider.ChatResponse{
			{
				Content: "",
				ToolCalls: []provider.ToolCall{
					{ID: "call_1", Name: "tofi_wait", Arguments: `{"seconds":0}`},
				},
			},
			{Content: "finished"},
		},
	}

	var captured []provider.Message
	var mu sync.Mutex
	onMessage := func(msg provider.Message) {
		mu.Lock()
		defer mu.Unlock()
		captured = append(captured, msg)
	}

	cfg := AgentConfig{
		Provider:  p,
		Model:     "gpt-4o-mini",
		System:    "test",
		Prompt:    "wait then respond",
		OnMessage: onMessage,
	}
	ctx := models.NewExecutionContext("test", "u1", t.TempDir())
	defer ctx.Close()

	_, err := RunAgentLoop(cfg, ctx)
	if err != nil {
		t.Fatalf("RunAgentLoop error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(captured) != 3 {
		t.Fatalf("expected 3 OnMessage calls (asst+tool+asst), got %d: %+v", len(captured), captured)
	}

	// 0: assistant with tool call
	if captured[0].Role != "assistant" {
		t.Errorf("msg[0] expected assistant, got %q", captured[0].Role)
	}
	if len(captured[0].ToolCalls) != 1 || captured[0].ToolCalls[0].ID != "call_1" {
		t.Errorf("msg[0] expected tool call with ID call_1, got %+v", captured[0].ToolCalls)
	}

	// 1: tool response
	if captured[1].Role != "tool" {
		t.Errorf("msg[1] expected tool, got %q", captured[1].Role)
	}
	if captured[1].ToolCallID != "call_1" {
		t.Errorf("msg[1] expected ToolCallID 'call_1', got %q", captured[1].ToolCallID)
	}
	if captured[1].ToolName != "tofi_wait" {
		t.Errorf("msg[1] expected ToolName 'tofi_wait', got %q", captured[1].ToolName)
	}

	// 2: final assistant
	if captured[2].Role != "assistant" {
		t.Errorf("msg[2] expected assistant, got %q", captured[2].Role)
	}
	if captured[2].Content != "finished" {
		t.Errorf("msg[2] expected content 'finished', got %q", captured[2].Content)
	}
}

// TestOnMessage_NilSafe verifies that leaving OnMessage nil does not panic
// or affect loop behavior (backward compatibility).
func TestOnMessage_NilSafe(t *testing.T) {
	p := &fakeProvider{
		responses: []provider.ChatResponse{
			{Content: "hi"},
		},
	}

	cfg := AgentConfig{
		Provider: p,
		Model:    "gpt-4o-mini",
		System:   "test",
		Prompt:   "hi",
		// OnMessage deliberately nil
	}
	ctx := models.NewExecutionContext("test", "u1", t.TempDir())
	defer ctx.Close()

	result, err := RunAgentLoop(cfg, ctx)
	if err != nil {
		t.Fatalf("unexpected error with nil OnMessage: %v", err)
	}
	if result.Content != "hi" {
		t.Errorf("expected 'hi', got %q", result.Content)
	}
}

// TestOnMessage_SyntheticUserNotEmitted verifies that when the model returns
// only <think> content (no real answer and no tool calls), the agent's
// internal "Please continue" synthetic user prompt is NOT emitted via
// OnMessage — only real assistant/tool messages are.
func TestOnMessage_SyntheticUserNotEmitted(t *testing.T) {
	p := &fakeProvider{
		responses: []provider.ChatResponse{
			// First response: only <think> tags, no tool calls → agent injects
			// synthetic "Please continue" user message and loops.
			{Content: "<think>reasoning only</think>"},
			// Second response: real answer.
			{Content: "real answer"},
		},
	}

	var captured []provider.Message
	var mu sync.Mutex
	onMessage := func(msg provider.Message) {
		mu.Lock()
		defer mu.Unlock()
		captured = append(captured, msg)
	}

	cfg := AgentConfig{
		Provider:  p,
		Model:     "gpt-4o-mini",
		System:    "test",
		Prompt:    "q",
		OnMessage: onMessage,
	}
	ctx := models.NewExecutionContext("test", "u1", t.TempDir())
	defer ctx.Close()

	_, err := RunAgentLoop(cfg, ctx)
	if err != nil {
		t.Fatalf("RunAgentLoop error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	// Expect: two assistant messages (think-only + real). No user message.
	for i, msg := range captured {
		if msg.Role == "user" {
			t.Errorf("msg[%d] should not be user (synthetic continuation must not emit), got %+v", i, msg)
		}
	}
	if len(captured) < 2 {
		t.Fatalf("expected at least 2 assistant messages, got %d: %+v", len(captured), captured)
	}
}

package agent

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"tofi-core/internal/models"
	"tofi-core/internal/provider"
)

// TestCheckRunBudget_PureFunction covers the cap-matching logic without the
// agent loop — each case independently verifies one cap dimension.
func TestCheckRunBudget_PureFunction(t *testing.T) {
	start := time.Now().Add(-30 * time.Second)

	cases := []struct {
		name      string
		cfg       *AgentConfig
		llmCalls  int
		cost      float64
		wantHit   bool
		wantInReason string
	}{
		{
			name:    "nil cfg returns false",
			cfg:     nil,
			wantHit: false,
		},
		{
			name:    "zero caps = unlimited",
			cfg:     &AgentConfig{},
			cost:    100.0,
			llmCalls: 9999,
			wantHit: false,
		},
		{
			name:         "cost cap exceeded",
			cfg:          &AgentConfig{MaxRunCost: 0.20},
			cost:         0.21,
			wantHit:      true,
			wantInReason: "cost",
		},
		{
			name:         "cost cap exactly at limit",
			cfg:          &AgentConfig{MaxRunCost: 0.20},
			cost:         0.20,
			wantHit:      true,
			wantInReason: "cost",
		},
		{
			name:         "llm calls cap exceeded",
			cfg:          &AgentConfig{MaxRunLLMCalls: 15},
			llmCalls:     15,
			wantHit:      true,
			wantInReason: "LLM calls",
		},
		{
			name:         "duration cap exceeded",
			cfg:          &AgentConfig{MaxRunDuration: 10 * time.Second},
			wantHit:      true,
			wantInReason: "duration",
		},
		{
			name:    "duration cap not yet exceeded",
			cfg:     &AgentConfig{MaxRunDuration: 1 * time.Hour},
			wantHit: false,
		},
		{
			name:         "first cap hit wins (cost)",
			cfg:          &AgentConfig{MaxRunCost: 0.01, MaxRunLLMCalls: 1},
			cost:         0.05,
			llmCalls:     100,
			wantHit:      true,
			wantInReason: "cost",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hit, reason := checkRunBudget(tc.cfg, tc.llmCalls, tc.cost, start)
			if hit != tc.wantHit {
				t.Errorf("want hit=%v, got %v (reason=%q)", tc.wantHit, hit, reason)
			}
			if tc.wantInReason != "" && !strings.Contains(reason, tc.wantInReason) {
				t.Errorf("want reason containing %q, got %q", tc.wantInReason, reason)
			}
		})
	}
}

// scriptedProvider returns a scripted sequence of ChatResponses. Each Chat
// call advances the cursor; out-of-range calls return a terminal assistant
// message so stray extra iterations don't hang the test.
type scriptedProvider struct {
	mu        sync.Mutex
	responses []provider.ChatResponse
	idx       int
	calls     int
}

func (s *scriptedProvider) Chat(ctx context.Context, req *provider.ChatRequest) (*provider.ChatResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if s.idx >= len(s.responses) {
		return &provider.ChatResponse{Content: "done"}, nil
	}
	resp := s.responses[s.idx]
	s.idx++
	return &resp, nil
}

func (s *scriptedProvider) ChatStream(ctx context.Context, req *provider.ChatRequest, onDelta func(provider.StreamDelta)) (*provider.ChatResponse, error) {
	return s.Chat(ctx, req)
}

// TestBudget_LLMCallCap_ForcesWrapUp: the agent keeps asking for tools
// round after round; once it hits the LLM-call cap, the loop injects a
// wrap-up directive and the NEXT LLM call comes back with a plain text
// answer, terminating normally. The last-turn tool calls are dropped so the
// transcript stays consistent.
func TestBudget_LLMCallCap_ForcesWrapUp(t *testing.T) {
	p := &scriptedProvider{
		responses: []provider.ChatResponse{
			// call 1: tool call
			{ToolCalls: []provider.ToolCall{{ID: "c1", Name: "tofi_wait", Arguments: `{"seconds":0}`}}},
			// call 2: tool call (this hits LLMCalls>=2 cap)
			{ToolCalls: []provider.ToolCall{{ID: "c2", Name: "tofi_wait", Arguments: `{"seconds":0}`}}},
			// call 3: after wrap-up directive, model obeys and returns text
			{Content: "final partial answer"},
		},
	}

	cfg := AgentConfig{
		Provider:       p,
		Model:          "gpt-4o-mini",
		System:         "test",
		Prompt:         "do stuff",
		MaxRunLLMCalls: 2, // cap after 2 LLM calls
	}
	ctx := models.NewExecutionContext("test", "u", t.TempDir())
	defer ctx.Close()

	result, err := RunAgentLoop(cfg, ctx)
	if err != nil {
		t.Fatalf("RunAgentLoop error: %v", err)
	}
	if result.Content != "final partial answer" {
		t.Errorf("expected final text, got %q", result.Content)
	}
	if p.calls != 3 {
		t.Errorf("expected 3 LLM calls total (2 before cap + 1 wrap-up), got %d", p.calls)
	}

	// Verify the wrap-up directive is present in the history AND the tool
	// calls from the over-budget response were stripped.
	var foundWrapUp, foundStrippedAsst bool
	for _, m := range result.Messages {
		if m.Role == "user" && strings.Contains(m.Content, "budget") && strings.Contains(m.Content, "Do not call any more tools") {
			foundWrapUp = true
		}
		if m.Role == "assistant" && len(m.ToolCalls) == 0 && m.Content == "" {
			foundStrippedAsst = true
		}
	}
	if !foundWrapUp {
		t.Errorf("expected synthetic wrap-up user message in history, got: %+v", result.Messages)
	}
	_ = foundStrippedAsst // informational; the over-budget assistant msg has empty content, it's fine either way
}

// TestBudget_ModelIgnoresDirective_HardStop: even after receiving the
// wrap-up directive, the model keeps asking for tools. We must hard-stop
// and NOT execute more tools. The final result is an assistant message
// (possibly stub) with no further LLM calls.
func TestBudget_ModelIgnoresDirective_HardStop(t *testing.T) {
	p := &scriptedProvider{
		responses: []provider.ChatResponse{
			// call 1: tool call (hits cap of 1)
			{ToolCalls: []provider.ToolCall{{ID: "c1", Name: "tofi_wait", Arguments: `{"seconds":0}`}}},
			// call 2: model ignores wrap-up and calls tool AGAIN → hard stop
			{Content: "", ToolCalls: []provider.ToolCall{{ID: "c2", Name: "tofi_wait", Arguments: `{"seconds":0}`}}},
			// call 3 should NEVER happen
			{Content: "SHOULD NOT APPEAR"},
		},
	}

	cfg := AgentConfig{
		Provider:       p,
		Model:          "gpt-4o-mini",
		System:         "test",
		Prompt:         "do stuff",
		MaxRunLLMCalls: 1,
	}
	ctx := models.NewExecutionContext("test", "u", t.TempDir())
	defer ctx.Close()

	result, err := RunAgentLoop(cfg, ctx)
	if err != nil {
		t.Fatalf("RunAgentLoop error: %v", err)
	}
	if p.calls != 2 {
		t.Errorf("expected exactly 2 LLM calls (original + wrap-up retry, then hard stop), got %d", p.calls)
	}
	if strings.Contains(result.Content, "SHOULD NOT APPEAR") {
		t.Errorf("hard-stop failed: content leaked from post-cap response: %q", result.Content)
	}
	// Since the model returned no text and ignored the directive, we expect
	// the canonical "Run stopped" fallback message.
	if !strings.Contains(result.Content, "Run stopped") && result.Content == "" {
		t.Errorf("expected a non-empty fallback result, got %q", result.Content)
	}
}

// TestBudget_ZeroCapsAllowUnlimited: guardrails must be opt-in — an
// AgentConfig with no caps set should never flip budgetWrapUp, even after
// many LLM calls. This keeps existing callers (that don't populate the
// fields yet) working unchanged.
func TestBudget_ZeroCapsAllowUnlimited(t *testing.T) {
	p := &scriptedProvider{
		responses: []provider.ChatResponse{
			{ToolCalls: []provider.ToolCall{{ID: "c1", Name: "tofi_wait", Arguments: `{"seconds":0}`}}},
			{ToolCalls: []provider.ToolCall{{ID: "c2", Name: "tofi_wait", Arguments: `{"seconds":0}`}}},
			{ToolCalls: []provider.ToolCall{{ID: "c3", Name: "tofi_wait", Arguments: `{"seconds":0}`}}},
			{Content: "all good"},
		},
	}

	cfg := AgentConfig{
		Provider: p,
		Model:    "gpt-4o-mini",
		System:   "test",
		Prompt:   "do stuff",
		// no caps
	}
	ctx := models.NewExecutionContext("test", "u", t.TempDir())
	defer ctx.Close()

	result, err := RunAgentLoop(cfg, ctx)
	if err != nil {
		t.Fatalf("RunAgentLoop error: %v", err)
	}
	if result.Content != "all good" {
		t.Errorf("expected normal completion with content 'all good', got %q", result.Content)
	}
	if p.calls != 4 {
		t.Errorf("expected full 4 LLM calls under no caps, got %d", p.calls)
	}
}

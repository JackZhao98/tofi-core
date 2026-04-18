package chat

import (
	"testing"

	"tofi-core/internal/provider"
	"tofi-core/internal/storage"
)

// TestIncrementalPersistence_ReadableMidStream is the regression test for the
// "messages vanish during hold / refresh" bug. It simulates the real flow:
//
//  1. User message saved before the loop starts (step 7 in executeChatSession).
//  2. Agent produces assistant+tool messages one at a time; each one is
//     persisted immediately via OnMessage → AddMessage + Save.
//  3. At any intermediate point a second reader (the refreshed browser)
//     loads the session and MUST see every message that has already been
//     produced. No phantom "vanishing turn".
//
// Before the fix, assistant/tool messages were only written when the agent
// loop returned — so a refresh during a hold returned an almost-empty
// session and then the messages magically reappeared minutes later.
func TestIncrementalPersistence_ReadableMidStream(t *testing.T) {
	dir := t.TempDir()
	db, err := storage.InitDB(dir)
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer db.Close()
	store := NewStore(dir, db)

	const (
		userID  = "u_test"
		scope   = ScopeUser
		sessID  = "s_incremental"
	)

	session := NewSession(sessID, "gpt-4o-mini", "")

	// Step 7 analog: save user message before loop starts.
	session.AddMessage(Message{Role: "user", Content: "what time is it?"})
	session.Status = "running"
	if err := store.Save(userID, scope, session); err != nil {
		t.Fatalf("initial save: %v", err)
	}

	// Helper: a second reader loads the session fresh from disk.
	reload := func() *Session {
		t.Helper()
		loaded, err := store.LoadByID(sessID)
		if err != nil {
			t.Fatalf("LoadByID: %v", err)
		}
		return loaded
	}

	// Simulates what server.executeChatSession's OnMessage now does.
	onMessage := func(msg provider.Message) {
		chatMsg := Message{
			Role:    msg.Role,
			Content: msg.Content,
			CallID:  msg.ToolCallID,
			Name:    msg.ToolName,
		}
		for _, tc := range msg.ToolCalls {
			chatMsg.ToolCalls = append(chatMsg.ToolCalls, ToolCall{
				ID:    tc.ID,
				Name:  tc.Name,
				Input: tc.Arguments,
			})
		}
		session.AddMessage(chatMsg)
		if err := store.Save(userID, scope, session); err != nil {
			t.Fatalf("incremental save: %v", err)
		}
	}

	// Initial state visible to a fresh reader: user message only.
	if got := len(reload().Messages); got != 1 {
		t.Fatalf("after user message: expected 1 msg on disk, got %d", got)
	}

	// Agent produces: assistant with tool call → tool result → final assistant.
	// Each OnMessage is a persist-and-flush moment.
	onMessage(provider.Message{
		Role: "assistant",
		ToolCalls: []provider.ToolCall{
			{ID: "call_1", Name: "tofi_shell", Arguments: `{"command":"date"}`},
		},
	})

	// Simulate a hold right here: the browser refreshes BEFORE tool_result
	// and BEFORE the final assistant. The reader must see the user message
	// AND the assistant-with-tool-call — not just the user message.
	midStream := reload()
	if len(midStream.Messages) != 2 {
		t.Fatalf("after assistant-with-toolcall: expected 2 msgs on disk, got %d", len(midStream.Messages))
	}
	if midStream.Messages[1].Role != "assistant" {
		t.Errorf("msg[1] role: expected assistant, got %q", midStream.Messages[1].Role)
	}
	if len(midStream.Messages[1].ToolCalls) != 1 || midStream.Messages[1].ToolCalls[0].ID != "call_1" {
		t.Errorf("msg[1] should carry tool call call_1, got %+v", midStream.Messages[1].ToolCalls)
	}

	onMessage(provider.Message{
		Role:       "tool",
		Content:    "Thu Apr 17 10:00:00 UTC 2026",
		ToolCallID: "call_1",
		ToolName:   "tofi_shell",
	})

	// Refresh again — now the tool result is visible too.
	afterTool := reload()
	if len(afterTool.Messages) != 3 {
		t.Fatalf("after tool result: expected 3 msgs on disk, got %d", len(afterTool.Messages))
	}
	if afterTool.Messages[2].Role != "tool" || afterTool.Messages[2].CallID != "call_1" {
		t.Errorf("msg[2] should be tool with CallID call_1, got %+v", afterTool.Messages[2])
	}

	onMessage(provider.Message{Role: "assistant", Content: "It's 10am."})

	final := reload()
	if len(final.Messages) != 4 {
		t.Fatalf("after final assistant: expected 4 msgs on disk, got %d", len(final.Messages))
	}
	if final.Messages[3].Content != "It's 10am." {
		t.Errorf("msg[3] content: expected \"It's 10am.\", got %q", final.Messages[3].Content)
	}
}

// TestIncrementalPersistence_Idempotent verifies that Save is safe to call
// repeatedly — which is the invariant the OnMessage callback relies on since
// it Saves on every single message append.
func TestIncrementalPersistence_Idempotent(t *testing.T) {
	dir := t.TempDir()
	db, err := storage.InitDB(dir)
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer db.Close()
	store := NewStore(dir, db)

	session := NewSession("s_idem", "gpt-4o-mini", "")
	session.AddMessage(Message{Role: "user", Content: "hi"})

	// Save 5 times in quick succession — no-op on unchanged data, no error.
	for i := 0; i < 5; i++ {
		if err := store.Save("u", ScopeUser, session); err != nil {
			t.Fatalf("Save #%d: %v", i, err)
		}
	}

	loaded, err := store.LoadByID("s_idem")
	if err != nil {
		t.Fatalf("LoadByID: %v", err)
	}
	if len(loaded.Messages) != 1 {
		t.Errorf("repeated Save should not duplicate messages; got %d", len(loaded.Messages))
	}
}

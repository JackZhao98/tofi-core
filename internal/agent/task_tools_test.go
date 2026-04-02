package agent

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestBuildTaskTools_WithBgManager(t *testing.T) {
	bgm := NewBackgroundTaskManager()
	tools := buildTaskTools(bgm, nil)

	// Should have task_status but NOT ask_user (nil callback)
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool (task_status only), got %d", len(tools))
	}
	if tools[0].Schema.Name != "tofi_task_status" {
		t.Errorf("expected tofi_task_status, got %s", tools[0].Schema.Name)
	}
}

func TestBuildTaskTools_WithAskUser(t *testing.T) {
	bgm := NewBackgroundTaskManager()
	askFn := func(q string, opts []string) (string, error) { return "yes", nil }
	tools := buildTaskTools(bgm, askFn)

	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}

	names := map[string]bool{}
	for _, tt := range tools {
		names[tt.Schema.Name] = true
	}
	if !names["tofi_task_status"] {
		t.Error("missing tofi_task_status")
	}
	if !names["tofi_ask_user"] {
		t.Error("missing tofi_ask_user")
	}
}

func TestBuildTaskTools_NilBgManager(t *testing.T) {
	tools := buildTaskTools(nil, nil)
	if len(tools) != 0 {
		t.Errorf("expected 0 tools with nil bgManager, got %d", len(tools))
	}
}

func TestTaskStatus_NonBlocking_StillRunning(t *testing.T) {
	bgm := NewBackgroundTaskManager()
	tools := buildTaskTools(bgm, nil)
	handler := tools[0].Handler

	// Simulate a background task that's still running
	bgm.mu.Lock()
	bgm.seq++
	taskID := fmt.Sprintf("sh_%d", bgm.seq)
	bgm.tasks[taskID] = &BackgroundTask{
		ID:        taskID,
		Command:   "sleep 100",
		StartTime: time.Now(),
		Done:      make(chan ShellResult, 1),
	}
	bgm.mu.Unlock()

	result, err := handler(map[string]interface{}{
		"task_id": taskID,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !contains(result, "still running") {
		t.Errorf("expected 'still running' in result, got: %s", result)
	}
}

func TestTaskStatus_NonBlocking_Completed(t *testing.T) {
	bgm := NewBackgroundTaskManager()
	tools := buildTaskTools(bgm, nil)
	handler := tools[0].Handler

	// Simulate a completed background task
	doneCh := make(chan ShellResult, 1)
	doneCh <- ShellResult{
		Stdout:     "hello from background",
		ExitCode:   0,
		DurationMs: 500,
	}

	bgm.mu.Lock()
	bgm.seq++
	taskID := fmt.Sprintf("sh_%d", bgm.seq)
	bgm.tasks[taskID] = &BackgroundTask{
		ID:   taskID,
		Done: doneCh,
	}
	bgm.mu.Unlock()

	result, err := handler(map[string]interface{}{
		"task_id": taskID,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !contains(result, "completed") {
		t.Errorf("expected 'completed' in result, got: %s", result)
	}
	if !contains(result, "hello from background") {
		t.Errorf("expected output in result, got: %s", result)
	}
}

func TestTaskStatus_Blocking_Wait(t *testing.T) {
	bgm := NewBackgroundTaskManager()
	tools := buildTaskTools(bgm, nil)
	handler := tools[0].Handler

	doneCh := make(chan ShellResult, 1)

	bgm.mu.Lock()
	bgm.seq++
	taskID := fmt.Sprintf("sh_%d", bgm.seq)
	bgm.tasks[taskID] = &BackgroundTask{
		ID:   taskID,
		Done: doneCh,
	}
	bgm.mu.Unlock()

	// Complete task after 100ms
	go func() {
		time.Sleep(100 * time.Millisecond)
		doneCh <- ShellResult{
			Stdout:     "delayed result",
			ExitCode:   0,
			DurationMs: 100,
		}
	}()

	result, err := handler(map[string]interface{}{
		"task_id": taskID,
		"wait":    true,
		"timeout": float64(5),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !contains(result, "delayed result") {
		t.Errorf("expected 'delayed result', got: %s", result)
	}
}

func TestTaskStatus_Blocking_Timeout(t *testing.T) {
	bgm := NewBackgroundTaskManager()
	tools := buildTaskTools(bgm, nil)
	handler := tools[0].Handler

	bgm.mu.Lock()
	bgm.seq++
	taskID := fmt.Sprintf("sh_%d", bgm.seq)
	bgm.tasks[taskID] = &BackgroundTask{
		ID:   taskID,
		Done: make(chan ShellResult, 1), // never sends
	}
	bgm.mu.Unlock()

	result, err := handler(map[string]interface{}{
		"task_id": taskID,
		"wait":    true,
		"timeout": float64(1), // 1 second timeout
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !contains(result, "still running") {
		t.Errorf("expected 'still running' after timeout, got: %s", result)
	}
}

func TestTaskStatus_MissingTaskID(t *testing.T) {
	bgm := NewBackgroundTaskManager()
	tools := buildTaskTools(bgm, nil)
	handler := tools[0].Handler

	result, err := handler(map[string]interface{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !contains(result, "task_id is required") {
		t.Errorf("expected error message, got: %s", result)
	}
}

func TestAskUser_Success(t *testing.T) {
	var receivedQuestion string
	var receivedOptions []string
	var mu sync.Mutex

	askFn := func(q string, opts []string) (string, error) {
		mu.Lock()
		receivedQuestion = q
		receivedOptions = opts
		mu.Unlock()
		return "Yes, delete it", nil
	}

	bgm := NewBackgroundTaskManager()
	tools := buildTaskTools(bgm, askFn)

	var askHandler func(map[string]interface{}) (string, error)
	for _, tt := range tools {
		if tt.Schema.Name == "tofi_ask_user" {
			askHandler = tt.Handler
		}
	}
	if askHandler == nil {
		t.Fatal("tofi_ask_user not found")
	}

	result, err := askHandler(map[string]interface{}{
		"question": "Delete this folder?",
		"options":  []interface{}{"Yes", "No"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mu.Lock()
	if receivedQuestion != "Delete this folder?" {
		t.Errorf("wrong question received: %s", receivedQuestion)
	}
	if len(receivedOptions) != 2 || receivedOptions[0] != "Yes" {
		t.Errorf("wrong options received: %v", receivedOptions)
	}
	mu.Unlock()

	if !contains(result, "Yes, delete it") {
		t.Errorf("expected user answer in result, got: %s", result)
	}
}

func TestAskUser_UserDeclines(t *testing.T) {
	askFn := func(q string, opts []string) (string, error) {
		return "", fmt.Errorf("user declined to answer")
	}

	bgm := NewBackgroundTaskManager()
	tools := buildTaskTools(bgm, askFn)

	var askHandler func(map[string]interface{}) (string, error)
	for _, tt := range tools {
		if tt.Schema.Name == "tofi_ask_user" {
			askHandler = tt.Handler
		}
	}

	result, err := askHandler(map[string]interface{}{
		"question": "Are you sure?",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !contains(result, "did not respond") {
		t.Errorf("expected decline message, got: %s", result)
	}
}

func TestAskUser_MissingQuestion(t *testing.T) {
	askFn := func(q string, opts []string) (string, error) {
		return "answer", nil
	}

	bgm := NewBackgroundTaskManager()
	tools := buildTaskTools(bgm, askFn)

	var askHandler func(map[string]interface{}) (string, error)
	for _, tt := range tools {
		if tt.Schema.Name == "tofi_ask_user" {
			askHandler = tt.Handler
		}
	}

	result, _ := askHandler(map[string]interface{}{})
	if !contains(result, "question is required") {
		t.Errorf("expected error for missing question, got: %s", result)
	}
}

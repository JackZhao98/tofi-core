package chat

import (
	"fmt"
	"testing"
	"time"
)

func TestCompact_BelowThreshold(t *testing.T) {
	s := NewSession("s_test1", "gpt-4o", "")
	for i := 0; i < 50; i++ {
		s.AddMessage(Message{Role: "user", Content: fmt.Sprintf("msg %d", i)})
	}
	if s.Compact() {
		t.Fatal("should not compact when below threshold")
	}
	if len(s.Messages) != 50 {
		t.Fatalf("expected 50 messages, got %d", len(s.Messages))
	}
}

func TestCompact_AtThreshold(t *testing.T) {
	s := NewSession("s_test2", "gpt-4o", "")
	for i := 0; i < MaxSessionMessages; i++ {
		s.AddMessage(Message{Role: "user", Content: fmt.Sprintf("msg %d", i)})
	}
	if s.Compact() {
		t.Fatal("should not compact at exactly MaxSessionMessages")
	}
}

func TestCompact_AboveThreshold(t *testing.T) {
	s := NewSession("s_test3", "gpt-4o", "")
	total := MaxSessionMessages + 50
	for i := 0; i < total; i++ {
		s.AddMessage(Message{
			Role:      "user",
			Content:   fmt.Sprintf("message about topic %d", i),
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		})
	}

	if !s.Compact() {
		t.Fatal("should compact when above threshold")
	}
	if len(s.Messages) != CompactKeepMessages {
		t.Fatalf("expected %d messages after compact, got %d", CompactKeepMessages, len(s.Messages))
	}
	if s.Summary == "" {
		t.Fatal("summary should be populated after compact")
	}

	// Verify oldest remaining message is from the end of the original list
	expectedContent := fmt.Sprintf("message about topic %d", total-CompactKeepMessages)
	if s.Messages[0].Content != expectedContent {
		t.Fatalf("expected first message %q, got %q", expectedContent, s.Messages[0].Content)
	}
}

func TestCompact_PreservesExistingSummary(t *testing.T) {
	s := NewSession("s_test4", "gpt-4o", "")
	s.Summary = "Previous LLM-generated summary of conversation."

	for i := 0; i < MaxSessionMessages+10; i++ {
		s.AddMessage(Message{Role: "user", Content: fmt.Sprintf("msg %d", i)})
	}

	s.Compact()

	if s.Summary == "Previous LLM-generated summary of conversation." {
		t.Fatal("summary should be updated")
	}
	if len(s.Summary) < 50 {
		t.Fatal("summary too short, expected previous + new content")
	}
}

func TestCompact_Idempotent(t *testing.T) {
	s := NewSession("s_test5", "gpt-4o", "")
	for i := 0; i < MaxSessionMessages+20; i++ {
		s.AddMessage(Message{Role: "user", Content: fmt.Sprintf("msg %d", i)})
	}

	s.Compact()
	count1 := len(s.Messages)
	summary1 := s.Summary

	// Second call should be no-op
	if s.Compact() {
		t.Fatal("second compact should return false")
	}
	if len(s.Messages) != count1 {
		t.Fatal("message count should not change on second compact")
	}
	if s.Summary != summary1 {
		t.Fatal("summary should not change on second compact")
	}
}

func TestFirstLine(t *testing.T) {
	tests := []struct {
		input    string
		max      int
		expected string
	}{
		{"hello world", 80, "hello world"},
		{"line1\nline2\nline3", 80, "line1"},
		{"  hello  ", 80, "hello"},
		{"abcde", 3, "abc..."},
		{"", 80, ""},
		{"你好世界测试", 4, "你好世界..."},
	}
	for _, tt := range tests {
		got := firstLine(tt.input, tt.max)
		if got != tt.expected {
			t.Errorf("firstLine(%q, %d) = %q, want %q", tt.input, tt.max, got, tt.expected)
		}
	}
}

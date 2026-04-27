package server

import "testing"

func TestAutoLoadSystemSkillsOnlyForUserChat(t *testing.T) {
	tests := []struct {
		name  string
		scope string
		want  bool
	}{
		{name: "user chat", scope: "", want: true},
		{name: "app run", scope: "agent:app-soa-gh101-registration-monitor", want: false},
		{name: "agent chat", scope: "agent:stock-monitor", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := autoLoadSystemSkills(tt.scope); got != tt.want {
				t.Fatalf("autoLoadSystemSkills(%q) = %v, want %v", tt.scope, got, tt.want)
			}
		})
	}
}

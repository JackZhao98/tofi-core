package server

import "testing"

func TestAppNotificationContent(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantText string
		wantSend bool
	}{
		{name: "empty", input: "", wantText: "", wantSend: false},
		{name: "whitespace", input: " \n\t ", wantText: "", wantSend: false},
		{name: "no notify", input: "NO_NOTIFY", wantText: "", wantSend: false},
		{name: "tofi no notify", input: " tofi_no_notify\n", wantText: "", wantSend: false},
		{name: "alert", input: "\nRegistration is Open\n", wantText: "Registration is Open", wantSend: true},
		{name: "phrase is not sentinel", input: "Status changed; do not use NO_NOTIFY here.", wantText: "Status changed; do not use NO_NOTIFY here.", wantSend: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotText, gotSend := appNotificationContent(tt.input)
			if gotText != tt.wantText || gotSend != tt.wantSend {
				t.Fatalf("appNotificationContent(%q) = (%q, %v), want (%q, %v)", tt.input, gotText, gotSend, tt.wantText, tt.wantSend)
			}
		})
	}
}

package server

import "testing"

func TestAppRunScopeUsesFullAppID(t *testing.T) {
	got := appRunScope("soa-gh101-registration-monitor")
	want := "agent:app-soa-gh101-registration-monitor"
	if got != want {
		t.Fatalf("appRunScope() = %q, want %q", got, want)
	}
}

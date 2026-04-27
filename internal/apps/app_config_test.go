package apps

import (
	"encoding/json"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestAppConfigSchedulePreservesIntervalFields(t *testing.T) {
	input := []byte(`
id: soa-gh101-monitor
name: SOA GH101 Monitor
description: Watch SOA GH101 registration status.
schedule:
  timezone: America/Los_Angeles
  entries:
    - time: "06:00"
      end_time: "23:30"
      interval_min: 15
      repeat:
        type: daily
      enabled: true
      label: registration window
`)

	var cfg AppConfig
	if err := yaml.Unmarshal(input, &cfg); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}
	if cfg.Schedule == nil || len(cfg.Schedule.Entries) != 1 {
		t.Fatalf("expected one schedule entry, got %#v", cfg.Schedule)
	}

	entry := cfg.Schedule.Entries[0]
	if entry.EndTime != "23:30" || entry.IntervalMin != 15 || entry.Label != "registration window" {
		t.Fatalf("schedule interval fields were not preserved: %#v", entry)
	}

	raw, err := json.Marshal(cfg.Schedule)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if got := string(raw); !containsAll(got, `"end_time":"23:30"`, `"interval_min":15`) {
		t.Fatalf("marshaled schedule lost interval fields: %s", got)
	}
}

func containsAll(s string, needles ...string) bool {
	for _, needle := range needles {
		if !strings.Contains(s, needle) {
			return false
		}
	}
	return true
}

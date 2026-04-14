package apps

import (
	"reflect"
	"sort"
	"testing"
)

func TestResolveWithOverrides_Precedence(t *testing.T) {
	defsJSON := `{
		"ticker": {"type": "text", "required": true},
		"horizon": {"type": "text", "default": "1m"},
		"include_crypto": {"type": "boolean", "default": "false"}
	}`
	savedJSON := `{"ticker": "AAPL", "horizon": "1w"}`

	tests := []struct {
		name     string
		tmpl     string
		runtime  map[string]interface{}
		expected string
	}{
		{
			name:     "runtime overrides saved",
			tmpl:     "Check {{ticker}} over {{horizon}}.",
			runtime:  map[string]interface{}{"ticker": "MSFT"},
			expected: "Check MSFT over 1w.",
		},
		{
			name:     "saved used when no runtime",
			tmpl:     "Check {{ticker}} over {{horizon}}.",
			runtime:  nil,
			expected: "Check AAPL over 1w.",
		},
		{
			// Existing resolveValue semantics: empty value ("") is treated as
			// "no value" and falls through to the parameter's default. This is
			// the historical behaviour shared with saved params; the runtime
			// override layer follows the same rule.
			name:     "empty runtime string falls through to default",
			tmpl:     "Check {{ticker}} over {{horizon}}.",
			runtime:  map[string]interface{}{"horizon": ""},
			expected: "Check AAPL over 1m.",
		},
		{
			// Conditional block content is TrimSpaced by the template engine.
			name:     "boolean runtime enables block",
			tmpl:     "Do X.{{#include_crypto}} Also crypto.{{/include_crypto}}",
			runtime:  map[string]interface{}{"include_crypto": true},
			expected: "Do X.Also crypto.",
		},
		{
			name:     "boolean default disables block",
			tmpl:     "Do X.{{#include_crypto}} Also crypto.{{/include_crypto}}",
			runtime:  nil,
			expected: "Do X.",
		},
		{
			name:     "number stringifies",
			tmpl:     "Top {{limit}} results.",
			runtime:  map[string]interface{}{"limit": 5},
			expected: "Top 5 results.",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolveWithOverrides(tc.tmpl, savedJSON, defsJSON, tc.runtime)
			if got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

func TestMissingRequiredParams(t *testing.T) {
	defsJSON := `{
		"ticker": {"type": "text", "required": true},
		"currency": {"type": "text", "required": true, "default": "USD"},
		"horizon": {"type": "text", "required": false}
	}`

	tests := []struct {
		name    string
		saved   string
		runtime map[string]interface{}
		missing []string
	}{
		{
			name:    "all missing when nothing provided",
			saved:   "",
			runtime: nil,
			missing: []string{"ticker"}, // currency has default, horizon not required
		},
		{
			name:    "saved satisfies",
			saved:   `{"ticker": "AAPL"}`,
			runtime: nil,
			missing: nil,
		},
		{
			name:    "runtime satisfies",
			saved:   "",
			runtime: map[string]interface{}{"ticker": "MSFT"},
			missing: nil,
		},
		{
			name:    "empty runtime does not satisfy",
			saved:   "",
			runtime: map[string]interface{}{"ticker": ""},
			missing: []string{"ticker"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := MissingRequiredParams(tc.saved, defsJSON, tc.runtime)
			sort.Strings(got)
			sort.Strings(tc.missing)
			if !reflect.DeepEqual(got, tc.missing) {
				t.Errorf("got %v, want %v", got, tc.missing)
			}
		})
	}
}

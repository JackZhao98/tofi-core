package server

import (
	"fmt"
	"time"

	"tofi-core/internal/agent"
	"tofi-core/internal/provider"
)

// buildBuiltinTools creates always-available utility tools for agents and chat.
func (s *Server) buildBuiltinTools(userID string) []agent.ExtraBuiltinTool {
	return []agent.ExtraBuiltinTool{
		buildGetTimeTool(),
		buildGetUserTool(userID),
	}
}

func buildGetTimeTool() agent.ExtraBuiltinTool {
	return agent.ExtraBuiltinTool{
		Schema: provider.Tool{
			Name:        "tofi_get_time",
			Description: "Get the current date, time, and timezone. Use this when you need the exact current time, especially during long-running tasks where the initial time in the system prompt may be stale.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"timezone": map[string]any{
						"type":        "string",
						"description": "IANA timezone name (e.g. 'America/New_York', 'Asia/Shanghai'). Defaults to server's local timezone.",
					},
				},
			},
		},
		Handler: func(args map[string]any) (string, error) {
			loc := time.Local
			if tz, ok := args["timezone"].(string); ok && tz != "" {
				parsed, err := time.LoadLocation(tz)
				if err != nil {
					return "", fmt.Errorf("invalid timezone %q: %w", tz, err)
				}
				loc = parsed
			}

			now := time.Now().In(loc)
			return fmt.Sprintf(`{"datetime": "%s", "unix": %d, "timezone": "%s", "weekday": "%s"}`,
				now.Format("2006-01-02 15:04:05"),
				now.Unix(),
				loc.String(),
				now.Weekday().String(),
			), nil
		},
	}
}

func buildGetUserTool(userID string) agent.ExtraBuiltinTool {
	return agent.ExtraBuiltinTool{
		Schema: provider.Tool{
			Name:        "tofi_get_user",
			Description: "Get the current user's identity. Returns the user ID only. Do NOT use this for token usage, cost, or chat statistics — use tofi_session_info instead.",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		Handler: func(args map[string]any) (string, error) {
			return fmt.Sprintf(`{"user_id": "%s"}`, userID), nil
		},
	}
}

package server

import "strings"

func appNotificationContent(content string) (string, bool) {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return "", false
	}

	switch strings.ToUpper(trimmed) {
	case "NO_NOTIFY", "TOFI_NO_NOTIFY", "__TOFI_NO_NOTIFY__":
		return "", false
	default:
		return trimmed, true
	}
}

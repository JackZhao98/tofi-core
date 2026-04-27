package server

import "tofi-core/internal/chat"

func appRunScope(appID string) string {
	return chat.AgentScope("app-" + appID)
}

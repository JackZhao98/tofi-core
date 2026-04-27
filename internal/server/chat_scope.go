package server

import "tofi-core/internal/chat"

func autoLoadSystemSkills(scope string) bool {
	return scope == chat.ScopeUser
}

package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"tofi-core/internal/storage"
)

// Usage-callback auth: skills run on the same host as the server and call
// back on loopback to report successful third-party API usage (e.g. a Brave
// search). The agent builder hands each skill invocation a single-use-style
// token scoped to the triggering user and the expected provider. The server
// validates the token, its expiry, and the loopback origin before appending
// to the service_usage table.

type usageTokenEntry struct {
	UserID    string
	Provider  string
	ExpiresAt time.Time
}

type usageTokenStore struct {
	mu     sync.Mutex
	tokens map[string]usageTokenEntry
}

func newUsageTokenStore() *usageTokenStore {
	store := &usageTokenStore{tokens: make(map[string]usageTokenEntry)}
	go store.reaper()
	return store
}

// issue mints a new callback token valid for `ttl` seconds. Returns the
// opaque token string the skill should send back with each usage event.
func (s *usageTokenStore) issue(userID, provider string, ttl time.Duration) string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return ""
	}
	token := hex.EncodeToString(buf)

	s.mu.Lock()
	s.tokens[token] = usageTokenEntry{
		UserID:    userID,
		Provider:  provider,
		ExpiresAt: time.Now().Add(ttl),
	}
	s.mu.Unlock()
	return token
}

// lookup returns the entry for a token if it exists and is unexpired.
func (s *usageTokenStore) lookup(token string) (usageTokenEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.tokens[token]
	if !ok {
		return usageTokenEntry{}, false
	}
	if time.Now().After(entry.ExpiresAt) {
		delete(s.tokens, token)
		return usageTokenEntry{}, false
	}
	return entry, true
}

// reaper runs in the background to prune expired tokens so long-lived
// servers don't accumulate stale entries.
func (s *usageTokenStore) reaper() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now()
		s.mu.Lock()
		for token, entry := range s.tokens {
			if now.After(entry.ExpiresAt) {
				delete(s.tokens, token)
			}
		}
		s.mu.Unlock()
	}
}

// isLoopbackRequest returns true if the request came from 127.0.0.1 or ::1.
// We reject callbacks from non-loopback interfaces — the token is injected
// into skill env on the same host, so remote callers cannot have a valid
// one unless the server was reconfigured to expose the endpoint publicly.
func isLoopbackRequest(r *http.Request) bool {
	host := r.RemoteAddr
	if idx := strings.LastIndex(host, ":"); idx >= 0 {
		host = host[:idx]
	}
	host = strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}

// handleInternalUsage POST /api/v1/internal/usage — token-gated callback
// used by skills (Python scripts) to report a successful third-party API
// call. Body: {"token": "...", "provider": "brave_search", "units": 1}.
func (s *Server) handleInternalUsage(w http.ResponseWriter, r *http.Request) {
	if !isLoopbackRequest(r) {
		writeJSONError(w, http.StatusForbidden, ErrForbidden, "internal endpoint only accepts loopback callers", "")
		return
	}

	var req struct {
		Token    string `json:"token"`
		Provider string `json:"provider"`
		Units    int    `json:"units"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, ErrBadRequest, "invalid body", "")
		return
	}
	if req.Token == "" {
		writeJSONError(w, http.StatusBadRequest, ErrBadRequest, "token required", "")
		return
	}

	entry, ok := s.usageTokens.lookup(req.Token)
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, ErrUnauthorized, "invalid or expired usage token", "")
		return
	}

	// The token is bound to a provider at issue time. Accept either an empty
	// provider (skill doesn't care) or an exact match — never let a token
	// issued for one service log usage under another.
	provider := req.Provider
	if provider == "" {
		provider = entry.Provider
	} else if provider != entry.Provider {
		writeJSONError(w, http.StatusForbidden, ErrForbidden, "token not valid for this provider", "")
		return
	}

	units := req.Units
	if units <= 0 {
		units = 1
	}
	if err := s.db.RecordServiceUsage(entry.UserID, provider, units); err != nil {
		writeJSONError(w, http.StatusInternalServerError, ErrInternal, "failed to record usage", "")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Compile-time interface check — storage must expose RecordServiceUsage.
var _ = (*storage.DB)(nil).RecordServiceUsage

package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"tofi-core/internal/storage"
)

// knownServiceProviders constrains which service keys admins can configure
// through the UI. The env var name is what the skill's SKILL.md exports, so
// the agent builder can resolve it on injection. Extend this table when new
// service-backed skills ship.
var knownServiceProviders = map[string]string{
	"brave_search": "BRAVE_API_KEY",
}

type serviceKeyListEntry struct {
	Provider  string                     `json:"provider"`
	EnvVar    string                     `json:"env_var"`
	Configured bool                      `json:"configured"`
	MaskedKey string                     `json:"masked_key,omitempty"`
	UpdatedAt string                     `json:"updated_at,omitempty"`
	Usage     *storage.ServiceUsageStats `json:"usage,omitempty"`
}

// handleAdminListServiceKeys GET /api/v1/admin/service-keys — returns the
// known providers, whether each has a configured system-scope key, and the
// current usage stats. The UI renders one row per provider.
func (s *Server) handleAdminListServiceKeys(w http.ResponseWriter, r *http.Request) {
	existing, _ := s.db.ListServiceKeys("system")
	byProvider := make(map[string]storage.ServiceKeyInfo, len(existing))
	for _, info := range existing {
		byProvider[info.Provider] = info
	}

	entries := make([]serviceKeyListEntry, 0, len(knownServiceProviders))
	for provider, envVar := range knownServiceProviders {
		e := serviceKeyListEntry{Provider: provider, EnvVar: envVar}
		if info, ok := byProvider[provider]; ok {
			e.Configured = true
			e.MaskedKey = info.MaskedKey
			e.UpdatedAt = info.UpdatedAt
		}
		if stats, err := s.db.GetServiceUsageStats(provider); err == nil {
			e.Usage = stats
		}
		entries = append(entries, e)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"service_keys": entries})
}

// handleAdminSetServiceKey PUT /api/v1/admin/service-keys/{provider} —
// admin-only. Body: {"api_key": "..."}.
func (s *Server) handleAdminSetServiceKey(w http.ResponseWriter, r *http.Request) {
	provider := r.PathValue("provider")
	if _, ok := knownServiceProviders[provider]; !ok {
		writeJSONError(w, http.StatusBadRequest, ErrBadRequest,
			fmt.Sprintf("unknown service provider %q", provider),
			"Accepted providers: brave_search")
		return
	}

	var req struct {
		APIKey string `json:"api_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.APIKey == "" {
		writeJSONError(w, http.StatusBadRequest, ErrBadRequest, "api_key required", "")
		return
	}

	if err := s.db.SetServiceKey(provider, "system", req.APIKey); err != nil {
		writeJSONError(w, http.StatusInternalServerError, ErrInternal,
			fmt.Sprintf("failed to save service key: %v", err), "")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "provider": provider})
}

// handleAdminDeleteServiceKey DELETE /api/v1/admin/service-keys/{provider}
func (s *Server) handleAdminDeleteServiceKey(w http.ResponseWriter, r *http.Request) {
	provider := r.PathValue("provider")
	if _, ok := knownServiceProviders[provider]; !ok {
		writeJSONError(w, http.StatusBadRequest, ErrBadRequest,
			fmt.Sprintf("unknown service provider %q", provider), "")
		return
	}
	if err := s.db.DeleteServiceKey(provider, "system"); err != nil {
		writeJSONError(w, http.StatusInternalServerError, ErrInternal, err.Error(), "")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleAdminServiceKeyUsage GET /api/v1/admin/service-keys/{provider}/usage
func (s *Server) handleAdminServiceKeyUsage(w http.ResponseWriter, r *http.Request) {
	provider := r.PathValue("provider")
	if _, ok := knownServiceProviders[provider]; !ok {
		writeJSONError(w, http.StatusBadRequest, ErrBadRequest,
			fmt.Sprintf("unknown service provider %q", provider), "")
		return
	}
	stats, err := s.db.GetServiceUsageStats(provider)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, ErrInternal, err.Error(), "")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

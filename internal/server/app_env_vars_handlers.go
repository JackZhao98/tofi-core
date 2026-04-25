package server

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"regexp"

	"github.com/google/uuid"

	"tofi-core/internal/crypto"
)

// validEnvName matches conventional shell env var names: leading letter or
// underscore, followed by ASCII alphanumerics or underscores.
var validEnvName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type setAppEnvVarRequest struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type appEnvVarResponse struct {
	Name      string `json:"name"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// handleListAppEnvVars GET /api/v1/agents/{id}/env-vars
func (s *Server) handleListAppEnvVars(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(UserContextKey).(string)
	appID := r.PathValue("id")

	app, err := s.db.GetApp(appID)
	if err != nil || app.UserID != userID {
		writeJSONError(w, http.StatusNotFound, ErrAppNotFound, "app not found", "")
		return
	}

	records, err := s.db.ListAppEnvVars(appID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, ErrInternal, err.Error(), "")
		return
	}

	out := make([]appEnvVarResponse, 0, len(records))
	for _, rec := range records {
		out = append(out, appEnvVarResponse{
			Name:      rec.Name,
			UpdatedAt: rec.UpdatedAt.String,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"env_vars": out})
}

// handleSetAppEnvVar POST /api/v1/agents/{id}/env-vars
// Body: {"name": "RH_TOKEN", "value": "..."}
func (s *Server) handleSetAppEnvVar(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(UserContextKey).(string)
	appID := r.PathValue("id")

	app, err := s.db.GetApp(appID)
	if err != nil || app.UserID != userID {
		writeJSONError(w, http.StatusNotFound, ErrAppNotFound, "app not found", "")
		return
	}

	var req setAppEnvVarRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, ErrBadRequest, "invalid request body", "")
		return
	}
	if !validEnvName.MatchString(req.Name) {
		writeJSONError(w, http.StatusBadRequest, ErrBadRequest,
			"name must match [A-Za-z_][A-Za-z0-9_]*", "")
		return
	}
	if req.Value == "" {
		writeJSONError(w, http.StatusBadRequest, ErrBadRequest, "value cannot be empty", "")
		return
	}

	encrypted, err := crypto.Encrypt(req.Value)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, ErrInternal, "encryption failed", "")
		return
	}

	if err := s.db.SaveAppEnvVar(uuid.New().String(), appID, userID, req.Name, encrypted); err != nil {
		writeJSONError(w, http.StatusInternalServerError, ErrInternal, err.Error(), "")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(appEnvVarResponse{Name: req.Name})
}

// handleDeleteAppEnvVar DELETE /api/v1/agents/{id}/env-vars/{name}
func (s *Server) handleDeleteAppEnvVar(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(UserContextKey).(string)
	appID := r.PathValue("id")
	name := r.PathValue("name")

	app, err := s.db.GetApp(appID)
	if err != nil || app.UserID != userID {
		writeJSONError(w, http.StatusNotFound, ErrAppNotFound, "app not found", "")
		return
	}

	if err := s.db.DeleteAppEnvVar(appID, name); err != nil {
		if err == sql.ErrNoRows {
			writeJSONError(w, http.StatusNotFound, ErrNotFound, "env var not found", "")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, ErrInternal, err.Error(), "")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// resolveAppEnv merges user secrets and per-app env vars into a single map
// suitable for sandbox injection. Per-app values override user-level ones —
// this matches the GitHub-secrets style "scope cascades downward" model.
//
// User-level entries with the reserved `ai_key_*` / `service_key_*` prefixes
// are skipped here: those are routed through dedicated AI/service-key
// resolvers and don't belong in the generic shell environment.
func (s *Server) resolveAppEnv(userID, appID string) map[string]string {
	merged := make(map[string]string)

	// 1. User-level secrets (lowest precedence)
	if userRecs, err := s.db.ListSecrets(userID); err == nil {
		for _, r := range userRecs {
			if isReservedSecretName(r.Name) {
				continue
			}
			rec, err := s.db.GetSecret(userID, r.Name)
			if err != nil {
				continue
			}
			plain, err := crypto.Decrypt(rec.EncryptedValue)
			if err != nil {
				continue
			}
			merged[r.Name] = plain
		}
	}

	// 2. App-level env vars (override)
	if appID != "" {
		if appEnv, err := s.db.LoadAppEnvVars(appID, crypto.Decrypt); err == nil {
			for k, v := range appEnv {
				merged[k] = v
			}
		}
	}

	return merged
}

func isReservedSecretName(name string) bool {
	for _, prefix := range []string{"ai_key_", "service_key_"} {
		if len(name) > len(prefix) && name[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}


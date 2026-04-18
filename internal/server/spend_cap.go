package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// --- Per-run budget caps (the "single runaway run can't burn a day" guard) ---
//
// These are the defaults applied to every chat / app run that does not have
// a user-scoped or system-scoped override. They are deliberately tight for
// free tier; scaling up for paid tier is a config change, not a code change.
const (
	defaultMaxRunCost     = 0.20             // $0.20 real USD cost per run
	defaultMaxRunLLMCalls = 15               // 15 LLM API calls per run
	defaultMaxRunDuration = 3 * time.Minute  // 3 minutes wall-clock per run
)

// resolveRunCaps returns the per-run budget to enforce for userID. The
// resolution order is:
//  1. Hard-coded defaults (free tier).
//  2. System-scoped settings (admin default for all users).
//  3. User-scoped settings (admin override for a specific user).
// Each later layer overrides the earlier one per-field. A negative or
// unparseable value is ignored.
func (s *Server) resolveRunCaps(userID string) (float64, int, time.Duration) {
	cost := defaultMaxRunCost
	llm := defaultMaxRunLLMCalls
	dur := defaultMaxRunDuration

	readFloat := func(scope, key string) (float64, bool) {
		v, err := s.db.GetSetting(key, scope)
		if err != nil || v == "" {
			return 0, false
		}
		x, err := strconv.ParseFloat(v, 64)
		if err != nil || x <= 0 {
			return 0, false
		}
		return x, true
	}
	readInt := func(scope, key string) (int, bool) {
		v, err := s.db.GetSetting(key, scope)
		if err != nil || v == "" {
			return 0, false
		}
		x, err := strconv.Atoi(v)
		if err != nil || x <= 0 {
			return 0, false
		}
		return x, true
	}

	for _, scope := range []string{"system", userID} {
		if x, ok := readFloat(scope, "run_cap_cost"); ok {
			cost = x
		}
		if x, ok := readInt(scope, "run_cap_llm_calls"); ok {
			llm = x
		}
		if x, ok := readInt(scope, "run_cap_duration_sec"); ok {
			dur = time.Duration(x) * time.Second
		}
	}

	return cost, llm, dur
}

// checkSpendCap checks if user has exceeded daily or monthly spend limits.
// Returns nil if within limits or no limits are set.
func (s *Server) checkSpendCap(userID string) error {
	now := time.Now().UTC()

	// Check daily cap
	if capStr, err := s.db.GetSetting("spend_cap_daily", userID); err == nil && capStr != "" {
		cap, err := strconv.ParseFloat(capStr, 64)
		if err == nil && cap > 0 {
			todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
			spent, err := s.db.GetUserSpend(userID, todayStart)
			if err == nil && spent >= cap {
				return fmt.Errorf("daily spend cap exceeded: $%.2f / $%.2f", spent, cap)
			}
		}
	}

	// Check monthly cap
	if capStr, err := s.db.GetSetting("spend_cap_monthly", userID); err == nil && capStr != "" {
		cap, err := strconv.ParseFloat(capStr, 64)
		if err == nil && cap > 0 {
			monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
			spent, err := s.db.GetUserSpend(userID, monthStart)
			if err == nil && spent >= cap {
				return fmt.Errorf("monthly spend cap exceeded: $%.2f / $%.2f", spent, cap)
			}
		}
	}

	return nil
}

// handleSetUserSpendCap sets the user's own spend cap.
// PUT /api/v1/user/settings/spend-cap
func (s *Server) handleSetUserSpendCap(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(UserContextKey).(string)

	var req struct {
		Daily   *float64 `json:"daily"`
		Monthly *float64 `json:"monthly"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, ErrBadRequest, "Invalid request body", "")
		return
	}

	if req.Daily != nil {
		if *req.Daily <= 0 {
			s.db.DeleteSetting("spend_cap_daily", userID)
		} else {
			s.db.SetSetting("spend_cap_daily", userID, fmt.Sprintf("%.2f", *req.Daily))
		}
	}
	if req.Monthly != nil {
		if *req.Monthly <= 0 {
			s.db.DeleteSetting("spend_cap_monthly", userID)
		} else {
			s.db.SetSetting("spend_cap_monthly", userID, fmt.Sprintf("%.2f", *req.Monthly))
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
}

// handleGetUserSpendCap returns the user's spend cap and current spend.
// GET /api/v1/user/settings/spend-cap
func (s *Server) handleGetUserSpendCap(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(UserContextKey).(string)
	now := time.Now().UTC()

	resp := map[string]interface{}{}

	// Daily
	if capStr, err := s.db.GetSetting("spend_cap_daily", userID); err == nil && capStr != "" {
		if cap, err := strconv.ParseFloat(capStr, 64); err == nil {
			todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
			spent, _ := s.db.GetUserSpend(userID, todayStart)
			resp["daily"] = map[string]interface{}{"cap": cap, "spent": spent}
		}
	}

	// Monthly
	if capStr, err := s.db.GetSetting("spend_cap_monthly", userID); err == nil && capStr != "" {
		if cap, err := strconv.ParseFloat(capStr, 64); err == nil {
			monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
			spent, _ := s.db.GetUserSpend(userID, monthStart)
			resp["monthly"] = map[string]interface{}{"cap": cap, "spent": spent}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleSetSystemSpendCap sets the system-wide default spend cap (admin only).
// PUT /api/v1/admin/settings/spend-cap
func (s *Server) handleSetSystemSpendCap(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Daily   *float64 `json:"daily"`
		Monthly *float64 `json:"monthly"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, ErrBadRequest, "Invalid request body", "")
		return
	}

	if req.Daily != nil {
		if *req.Daily <= 0 {
			s.db.DeleteSetting("spend_cap_daily", "system")
		} else {
			s.db.SetSetting("spend_cap_daily", "system", fmt.Sprintf("%.2f", *req.Daily))
		}
	}
	if req.Monthly != nil {
		if *req.Monthly <= 0 {
			s.db.DeleteSetting("spend_cap_monthly", "system")
		} else {
			s.db.SetSetting("spend_cap_monthly", "system", fmt.Sprintf("%.2f", *req.Monthly))
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
}

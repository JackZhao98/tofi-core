package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"tofi-core/internal/apps"
	"tofi-core/internal/storage"

	"github.com/google/uuid"
)

// ── App CRUD Handlers ──

// handleListApps GET /api/v1/apps
func (s *Server) handleListApps(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(UserContextKey).(string)

	apps, err := s.db.ListApps(userID)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to list apps: %v", err), http.StatusInternalServerError)
		return
	}
	if apps == nil {
		apps = []*storage.AppRecord{}
	}

	type AppWithMeta struct {
		*storage.AppRecord
		PendingRuns int    `json:"pending_runs"`
		NextRunAt   string `json:"next_run_at,omitempty"`
	}

	result := make([]AppWithMeta, len(apps))
	for i, a := range apps {
		result[i] = AppWithMeta{AppRecord: a}
		runs, err := s.db.ListAppRuns(a.ID, "pending", 1)
		if err == nil && len(runs) > 0 {
			result[i].NextRunAt = runs[0].ScheduledAt
		}
		count, err := s.db.CountPendingAppRuns(a.ID)
		if err == nil {
			result[i].PendingRuns = count
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// handleGetApp GET /api/v1/apps/{id}
func (s *Server) handleGetApp(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(UserContextKey).(string)
	id := r.PathValue("id")

	app, err := s.db.GetApp(id)
	if err != nil {
		http.Error(w, "app not found", http.StatusNotFound)
		return
	}
	if app.UserID != userID {
		http.Error(w, "app not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(app)
}

// handleCreateApp POST /api/v1/apps
func (s *Server) handleCreateApp(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(UserContextKey).(string)

	var req struct {
		Name             string           `json:"name"`
		Description      string           `json:"description"`
		Prompt           string           `json:"prompt"`
		SystemPrompt     string           `json:"system_prompt"`
		Model            string           `json:"model"`
		Skills           []string         `json:"skills"`
		ScheduleRules    *json.RawMessage `json:"schedule_rules"`
		Capabilities     *json.RawMessage `json:"capabilities"`
		BufferSize       *int             `json:"buffer_size"`
		RenewalThreshold *int             `json:"renewal_threshold"`
		Parameters       *json.RawMessage `json:"parameters"`
		ParameterDefs    *json.RawMessage `json:"parameter_defs"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	skillsJSON, _ := json.Marshal(req.Skills)
	if req.Skills == nil {
		skillsJSON = []byte("[]")
	}

	scheduleRules := "[]"
	if req.ScheduleRules != nil {
		scheduleRules = string(*req.ScheduleRules)
	}

	capabilities := "{}"
	if req.Capabilities != nil {
		capabilities = string(*req.Capabilities)
	}

	parameters := "{}"
	if req.Parameters != nil {
		parameters = string(*req.Parameters)
	}

	parameterDefs := "{}"
	if req.ParameterDefs != nil {
		parameterDefs = string(*req.ParameterDefs)
	}

	bufferSize := 20
	if req.BufferSize != nil {
		bufferSize = *req.BufferSize
	}
	renewalThreshold := 5
	if req.RenewalThreshold != nil {
		renewalThreshold = *req.RenewalThreshold
	}

	app := &storage.AppRecord{
		ID:               uuid.New().String(),
		Name:             req.Name,
		Description:      req.Description,
		Prompt:           req.Prompt,
		SystemPrompt:     req.SystemPrompt,
		Model:            req.Model,
		Skills:           string(skillsJSON),
		ScheduleRules:    scheduleRules,
		Capabilities:     capabilities,
		BufferSize:       bufferSize,
		RenewalThreshold: renewalThreshold,
		IsActive:         false,
		UserID:           userID,
		Parameters:       parameters,
		ParameterDefs:    parameterDefs,
	}

	if err := s.db.CreateApp(app); err != nil {
		http.Error(w, fmt.Sprintf("failed to create app: %v", err), http.StatusInternalServerError)
		return
	}

	created, err := s.db.GetApp(app.ID)
	if err != nil {
		created = app
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(created)
}

// handleUpdateApp PUT /api/v1/apps/{id}
func (s *Server) handleUpdateApp(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(UserContextKey).(string)
	id := r.PathValue("id")

	existing, err := s.db.GetApp(id)
	if err != nil || existing.UserID != userID {
		http.Error(w, "app not found", http.StatusNotFound)
		return
	}

	var req struct {
		Name             *string          `json:"name"`
		Description      *string          `json:"description"`
		Prompt           *string          `json:"prompt"`
		SystemPrompt     *string          `json:"system_prompt"`
		Model            *string          `json:"model"`
		Skills           []string         `json:"skills"`
		ScheduleRules    *json.RawMessage `json:"schedule_rules"`
		Capabilities     *json.RawMessage `json:"capabilities"`
		BufferSize       *int             `json:"buffer_size"`
		RenewalThreshold *int             `json:"renewal_threshold"`
		Parameters       *json.RawMessage `json:"parameters"`
		ParameterDefs    *json.RawMessage `json:"parameter_defs"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Name != nil {
		existing.Name = *req.Name
	}
	if req.Description != nil {
		existing.Description = *req.Description
	}
	if req.Prompt != nil {
		existing.Prompt = *req.Prompt
	}
	if req.SystemPrompt != nil {
		existing.SystemPrompt = *req.SystemPrompt
	}
	if req.Model != nil {
		existing.Model = *req.Model
	}
	if req.Skills != nil {
		skillsJSON, _ := json.Marshal(req.Skills)
		existing.Skills = string(skillsJSON)
	}
	if req.ScheduleRules != nil {
		existing.ScheduleRules = string(*req.ScheduleRules)
	}
	if req.Capabilities != nil {
		existing.Capabilities = string(*req.Capabilities)
	}
	if req.BufferSize != nil {
		existing.BufferSize = *req.BufferSize
	}
	if req.RenewalThreshold != nil {
		existing.RenewalThreshold = *req.RenewalThreshold
	}
	if req.Parameters != nil {
		existing.Parameters = string(*req.Parameters)
	}
	if req.ParameterDefs != nil {
		existing.ParameterDefs = string(*req.ParameterDefs)
	}

	if err := s.db.UpdateApp(existing); err != nil {
		http.Error(w, fmt.Sprintf("failed to update app: %v", err), http.StatusInternalServerError)
		return
	}

	// If app is active and schedule changed, reschedule
	if existing.IsActive && req.ScheduleRules != nil && s.appScheduler != nil {
		s.appScheduler.RemoveApp(id)
		s.db.CancelPendingAppRuns(id)
		if err := s.appScheduler.ActivateApp(existing); err != nil {
			log.Printf("Failed to reschedule app %s: %v", id, err)
		}
	}

	updated, _ := s.db.GetApp(id)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(updated)
}

// handleDeleteApp DELETE /api/v1/apps/{id}
func (s *Server) handleDeleteApp(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(UserContextKey).(string)
	id := r.PathValue("id")

	if s.appScheduler != nil {
		s.appScheduler.RemoveApp(id)
	}

	if err := s.db.DeleteApp(id, userID); err != nil {
		http.Error(w, fmt.Sprintf("failed to delete app: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ── Schedule Handlers ──

// handleActivateApp POST /api/v1/apps/{id}/activate
func (s *Server) handleActivateApp(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(UserContextKey).(string)
	id := r.PathValue("id")

	app, err := s.db.GetApp(id)
	if err != nil || app.UserID != userID {
		http.Error(w, "app not found", http.StatusNotFound)
		return
	}

	if app.ScheduleRules == "" || app.ScheduleRules == "[]" {
		http.Error(w, "app has no schedule rules configured", http.StatusBadRequest)
		return
	}

	if err := s.db.SetAppActive(id, userID, true); err != nil {
		http.Error(w, fmt.Sprintf("failed to activate: %v", err), http.StatusInternalServerError)
		return
	}

	if s.appScheduler != nil {
		app.IsActive = true
		if err := s.appScheduler.ActivateApp(app); err != nil {
			log.Printf("Failed to activate app %s in scheduler: %v", id, err)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status":  "activated",
		"message": "App schedule activated",
	})
}

// handleDeactivateApp POST /api/v1/apps/{id}/deactivate
func (s *Server) handleDeactivateApp(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(UserContextKey).(string)
	id := r.PathValue("id")

	app, err := s.db.GetApp(id)
	if err != nil || app.UserID != userID {
		http.Error(w, "app not found", http.StatusNotFound)
		return
	}

	if err := s.db.SetAppActive(id, userID, false); err != nil {
		http.Error(w, fmt.Sprintf("failed to deactivate: %v", err), http.StatusInternalServerError)
		return
	}

	cancelled, _ := s.db.CancelPendingAppRuns(id)

	if s.appScheduler != nil {
		s.appScheduler.RemoveApp(id)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status":    "deactivated",
		"cancelled": cancelled,
	})
}

// handleRunAppNow POST /api/v1/apps/{id}/run
func (s *Server) handleRunAppNow(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(UserContextKey).(string)
	id := r.PathValue("id")

	app, err := s.db.GetApp(id)
	if err != nil || app.UserID != userID {
		http.Error(w, "app not found", http.StatusNotFound)
		return
	}

	if app.Prompt == "" {
		http.Error(w, "app has no prompt configured", http.StatusBadRequest)
		return
	}

	card, err := s.createAndExecuteAppCard(app, userID)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to run app: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(card)
}

// handleListAppRuns GET /api/v1/apps/{id}/runs
func (s *Server) handleListAppRuns(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(UserContextKey).(string)
	id := r.PathValue("id")

	app, err := s.db.GetApp(id)
	if err != nil || app.UserID != userID {
		http.Error(w, "app not found", http.StatusNotFound)
		return
	}

	status := r.URL.Query().Get("status")
	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	runs, err := s.db.ListAppRuns(id, status, limit)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to list runs: %v", err), http.StatusInternalServerError)
		return
	}
	if runs == nil {
		runs = []*storage.AppRunRecord{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(runs)
}

// handleParseSchedule POST /api/v1/apps/parse-schedule
func (s *Server) handleParseSchedule(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(UserContextKey).(string)

	var req struct {
		Text     string           `json:"text"`
		Timezone string           `json:"timezone"`
		Existing json.RawMessage  `json:"existing,omitempty"` // existing entries for smart merge
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Text == "" {
		http.Error(w, "text is required", http.StatusBadRequest)
		return
	}
	if req.Timezone == "" {
		req.Timezone = "America/New_York"
	}

	systemPrompt := `You are a schedule editor. Convert natural language into structured schedule entries, and intelligently merge with existing entries when provided.

Output ONLY valid JSON in this exact format:
{
  "entries": [
    {
      "time": "09:00",
      "repeat": { "type": "weekly", "days": ["mon", "tue", "wed", "thu", "fri"] },
      "enabled": true
    }
  ],
  "timezone": "Asia/Shanghai"
}

Entry fields:
- "time": required, HH:MM (24h), the run time
- "end_time": optional, HH:MM, only if interval_min > 0 (time window end)
- "interval_min": optional, minutes between runs within a window. 0 or omitted = run once at "time"
- "repeat": required object with:
  - "type": one of "daily", "weekly", "monthly", "once"
  - "days": for weekly, array of "mon","tue","wed","thu","fri","sat","sun"
  - "dates": for monthly, array of day numbers [1, 15, 28]
  - "date": for once, "YYYY-MM-DD" format
- "enabled": always true for new entries
- "label": optional short description

MERGE RULES (when existing entries are provided):
- Default: ADD new entries, keep all existing entries untouched
- Only MODIFY an existing entry if the user clearly refers to changing it (e.g. "把早上8点改成9点", "change 8am to 9am")
- Only REMOVE entries if the user explicitly says to remove, delete, or replace all
- When ambiguous, prefer adding over modifying
- Always preserve existing entries' enabled state

Examples:
- "每天早上9点" → entries:[{time:"09:00", repeat:{type:"daily"}, enabled:true}]
- "工作日9点到17点每小时" → entries:[{time:"09:00", end_time:"17:00", interval_min:60, repeat:{type:"weekly", days:["mon","tue","wed","thu","fri"]}, enabled:true}]
- "每月1号和15号下午3点" → entries:[{time:"15:00", repeat:{type:"monthly", dates:[1,15]}, enabled:true}]
- "3月15日下午2点" → entries:[{time:"14:00", repeat:{type:"once", date:"2026-03-15"}, enabled:true}]`

	// Build user prompt with existing entries context
	var promptParts []string
	promptParts = append(promptParts, fmt.Sprintf("Timezone: %s", req.Timezone))

	if len(req.Existing) > 0 && string(req.Existing) != "null" && string(req.Existing) != "[]" {
		promptParts = append(promptParts, fmt.Sprintf("\nEXISTING ENTRIES:\n%s", string(req.Existing)))
	}

	promptParts = append(promptParts, fmt.Sprintf("\nUser request: %s", req.Text))
	prompt := strings.Join(promptParts, "\n")

	model, apiKey, provider, err := s.resolveModelAndKey(userID, "")
	if err != nil {
		http.Error(w, fmt.Sprintf("no API key available: %v", err), http.StatusInternalServerError)
		return
	}

	result, err := callLLM(systemPrompt, prompt, apiKey, model, provider)
	if err != nil {
		http.Error(w, fmt.Sprintf("LLM call failed: %v", err), http.StatusInternalServerError)
		return
	}

	cleaned := cleanJSONResponse(result)

	var parsed json.RawMessage
	if err := json.Unmarshal([]byte(cleaned), &parsed); err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"raw":   result,
			"error": "LLM response is not valid JSON, please try again",
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(parsed)
}

// cleanJSONResponse strips markdown code fences from LLM output
func cleanJSONResponse(s string) string {
	if len(s) > 6 && s[:3] == "```" {
		end := len(s) - 1
		for end > 0 && s[end] != '`' {
			end--
		}
		if end > 3 {
			start := 3
			for start < len(s) && s[start] != '\n' {
				start++
			}
			if start < end {
				s = s[start+1 : end-2]
			}
		}
	}
	return s
}

// createAndExecuteAppCard creates a KanbanCard from an App and executes it
func (s *Server) createAndExecuteAppCard(app *storage.AppRecord, userID string) (*storage.KanbanCardRecord, error) {
	// Resolve prompt template with parameter values
	prompt := apps.ResolveFromJSON(app.Prompt, app.Parameters, app.ParameterDefs)

	card := &storage.KanbanCardRecord{
		ID:          uuid.New().String(),
		Title:       prompt,
		Description: fmt.Sprintf("[App: %s] %s", app.Name, app.Description),
		Status:      "todo",
		AppID:       app.ID,
		AgentID:     app.ID, // backward compat
		UserID:      userID,
	}

	if err := s.db.CreateKanbanCard(card); err != nil {
		return nil, err
	}

	created, _ := s.db.GetKanbanCard(card.ID)
	if created == nil {
		created = card
	}

	go s.executeAppCard(created, app, userID)

	return created, nil
}

// executeAppCard executes a KanbanCard using App configuration
func (s *Server) executeAppCard(card *storage.KanbanCardRecord, app *storage.AppRecord, userID string) {
	requestedModel := app.Model
	s.executeWish(card, userID, requestedModel)
}

// ── Schedules Handlers ──

// handleGetUpcomingRuns GET /api/v1/schedules/upcoming
func (s *Server) handleGetUpcomingRuns(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(UserContextKey).(string)

	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	runs, err := s.db.GetUpcomingRuns(userID, limit)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to get upcoming runs: %v", err), http.StatusInternalServerError)
		return
	}
	if runs == nil {
		runs = []*storage.UpcomingRunRecord{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(runs)
}

// handleSkipRun POST /api/v1/schedules/{runId}/skip
func (s *Server) handleSkipRun(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(UserContextKey).(string)
	runID := r.PathValue("runId")

	if err := s.db.SkipAppRun(runID, userID); err != nil {
		http.Error(w, fmt.Sprintf("failed to skip run: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "skipped"})
}

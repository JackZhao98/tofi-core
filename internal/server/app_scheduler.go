package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"tofi-core/internal/apps"
	"tofi-core/internal/bridge"
	"tofi-core/internal/chat"
	"tofi-core/internal/connect"
	"tofi-core/internal/agent"
	"tofi-core/internal/storage"

	"github.com/google/uuid"
)

// ── Schedule Types (v2: entry-based alarm model) ──

type Schedule struct {
	Entries  []ScheduleEntry `json:"entries"`
	Timezone string          `json:"timezone"`
}

type ScheduleEntry struct {
	Time        string        `json:"time"`                   // "08:00"
	EndTime     string        `json:"end_time,omitempty"`     // "17:00" (only if interval)
	IntervalMin int           `json:"interval_min,omitempty"` // 0 = once at time
	Repeat      RepeatPattern `json:"repeat"`
	Enabled     bool          `json:"enabled"`
	Label       string        `json:"label,omitempty"`
}

type RepeatPattern struct {
	Type  string   `json:"type"`            // "daily", "weekly", "monthly", "once"
	Days  []string `json:"days,omitempty"`  // for weekly: ["mon","tue",...]
	Dates []int    `json:"dates,omitempty"` // for monthly: [1, 15]
	Date  string   `json:"date,omitempty"`  // for once: "2026-03-15"
}

// ── Legacy Schedule Types (v1: rules-based, kept for backward compat) ──

type ScheduleRule struct {
	Rules    []RuleEntry `json:"rules"`
	Timezone string      `json:"timezone"`
}

type RuleEntry struct {
	Days    []string     `json:"days"`    // ["mon","tue",...] empty = every day
	Windows []TimeWindow `json:"windows"`
}

type TimeWindow struct {
	Start       string `json:"start"`        // "09:00"
	End         string `json:"end"`          // "09:30"
	IntervalMin int    `json:"interval_min"` // 0 = run once at start
}

// ── App Scheduler (DB-poll based) ──

type AppScheduler struct {
	server  *Server
	mu      sync.Mutex // guards dispatch to prevent double-dispatch
	stopCh  chan struct{}
	stopped bool
}

func NewAppScheduler(server *Server) *AppScheduler {
	return &AppScheduler{
		server: server,
		stopCh: make(chan struct{}),
	}
}

func (as *AppScheduler) Start() error {
	go as.pollLoop()
	log.Println("⏰ App Scheduler started (DB-poll, 30s interval)")
	return nil
}

func (as *AppScheduler) Stop() {
	if as.stopped {
		return
	}
	as.stopped = true
	close(as.stopCh)
	log.Println("⏰ App Scheduler stopped")
}

func (as *AppScheduler) pollLoop() {
	// Run immediately on start
	as.pollAndDispatch()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			as.pollAndDispatch()
		case <-as.stopCh:
			return
		}
	}
}

func (as *AppScheduler) pollAndDispatch() {
	as.mu.Lock()
	defer as.mu.Unlock()

	// 1. Dispatch overdue pending runs (only for active apps)
	runs, err := as.server.db.GetPendingAppRunsDue(time.Now())
	if err != nil {
		log.Printf("[app-scheduler] Failed to query due runs: %v", err)
	} else {
		for _, r := range runs {
			// Mark running immediately to prevent double-dispatch on next poll
			if err := as.server.db.UpdateAppRunStatus(r.ID, "running"); err != nil {
				log.Printf("[app-scheduler] Failed to mark run %s as running: %v", r.ID[:8], err)
				continue
			}
			// Scheduled runs get a session hub too so any browser watching
			// /chat/{id} can tune into the SSE stream mid-execution. No HTTP
			// client is racing here (the trigger is the cron loop itself),
			// but the contract is symmetric with DispatchRun for manual /
			// webhook triggers — every agent loop gets one hub.
			agentCtx, cancel := context.WithCancel(context.Background())
			hub, hubErr := as.server.createSessionHub(r.SessionID, cancel)
			if hubErr != nil {
				log.Printf("[app-scheduler] session hub registration failed for %s: %v", r.ID[:8], hubErr)
				cancel()
			}
			go as.dispatchRun(r, "", nil, hub, agentCtx)
		}
	}

	// 2. Check renewals for active apps
	as.checkRenewals()
}

// DispatchManualRun creates an app_run record with trigger=manual and dispatches it immediately.
// If promptOverride is non-empty, it replaces the app's configured prompt for this run only.
// runtimeParams merges into the saved parameter values for this run (highest precedence).
func (as *AppScheduler) DispatchManualRun(app *storage.AppRecord, userID string, promptOverride string, runtimeParams map[string]interface{}) (*storage.AppRunRecord, error) {
	return as.DispatchRun(app, userID, promptOverride, "manual", runtimeParams)
}

// DispatchRun creates and executes a run with the given trigger type.
//
// Plan quota enforcement happens here so every dispatch entry point —
// webhook trigger, agent tofi_run_app tool, TUI manual trigger, and the
// scheduler's cron loop — goes through the same gate. Previously only the
// webhook handler checked a WebhookAPI flag; an agent with tofi_run_app
// could loop-trigger runs until monthly settings quota kicked in, bypassing
// the user's plan DailyRuns / ConcurrentRuns limits.
func (as *AppScheduler) DispatchRun(app *storage.AppRecord, userID, promptOverride, trigger string, runtimeParams map[string]interface{}) (*storage.AppRunRecord, error) {
	limits := as.server.getUserPlanLimits(userID)

	// Daily-runs quota is counted against the unified agent_runs log so
	// that chat turns, app triggers, webhook calls, and scheduled cron
	// runs all share the same ledger (see storage/agent_runs.go).
	if limits.DailyRuns > 0 {
		used, err := as.server.db.CountDailyAgentRuns(userID)
		if err == nil && used >= limits.DailyRuns {
			return nil, fmt.Errorf("daily run limit reached (%d/%d) — upgrade plan or wait until tomorrow", used, limits.DailyRuns)
		}
	}

	// Monthly-runs hard ceiling. Daily caps let bursty usage through; the
	// monthly cap is what prevents a steady-state user from sustaining a
	// cap-saturating pace for 30 days and blowing the COGS budget. 0 =
	// unlimited by the package-wide convention, so this check is a no-op
	// on plans that haven't rolled out a monthly cap yet.
	if limits.MonthlyRuns > 0 {
		used, err := as.server.db.CountMonthlyAgentRuns(userID)
		if err == nil && used >= limits.MonthlyRuns {
			return nil, fmt.Errorf("monthly run limit reached (%d/%d) — upgrade plan or wait until next month", used, limits.MonthlyRuns)
		}
	}

	// Concurrent-runs cap still uses app_runs since chat sessions are
	// interactive (one user watching) and should not occupy a slot.
	if limits.ConcurrentRuns > 0 {
		running, err := as.server.db.CountRunningRuns(userID)
		if err == nil && running >= limits.ConcurrentRuns {
			return nil, fmt.Errorf("concurrent run limit reached (%d/%d) — wait for a run to finish", running, limits.ConcurrentRuns)
		}
	}

	// Record the run in the unified ledger. A failure here is non-fatal
	// — we'd rather the agent actually run than reject the request on a
	// bookkeeping INSERT error.
	if err := as.server.db.RecordAgentRun(userID, trigger); err != nil {
		log.Printf("[app-run] ⚠️ RecordAgentRun failed for user %s: %v", userID, err)
	}

	// Pre-allocate a chat session ID so the HTTP caller can immediately
	// navigate to /chat/{session_id} — the session object itself is built
	// later inside dispatchRun's goroutine.
	run := &storage.AppRunRecord{
		ID:          uuid.New().String(),
		AppID:       app.ID,
		ScheduledAt: time.Now().UTC().Format("2006-01-02 15:04:05"),
		Status:      "running",
		Trigger:     trigger,
		SessionID:   "s_" + uuid.New().String()[:12],
		UserID:      userID,
	}
	if err := as.server.db.CreateAppRun(run); err != nil {
		return nil, fmt.Errorf("create app_run: %w", err)
	}
	// Mark running (started_at)
	as.server.db.UpdateAppRunStatus(run.ID, "running")

	// Register the session hub SYNCHRONOUSLY here, before starting the
	// goroutine. The HTTP response returns the session_id to the client,
	// which then navigates to /chat/{session_id} and almost immediately
	// POSTs /chat/sessions/{id}/stream to resume live SSE. If we registered
	// the hub inside the goroutine, there'd be a race where the client's
	// stream request lands before the hub exists and gets an 'idle' event
	// (nothing to subscribe to). Registering here eliminates that race —
	// by the time the HTTP response hits the wire, the hub is discoverable.
	agentCtx, cancel := context.WithCancel(context.Background())
	hub, hubErr := as.server.createSessionHub(run.SessionID, cancel)
	if hubErr != nil {
		// Collision means a hub already exists for this session ID —
		// shouldn't happen with a fresh UUID, but don't block the run.
		log.Printf("[app-run:%s] session hub already exists: %v", run.ID[:8], hubErr)
		cancel()
	}

	go as.dispatchRun(run, promptOverride, runtimeParams, hub, agentCtx)
	return run, nil
}

func (as *AppScheduler) dispatchRun(run *storage.AppRunRecord, promptOverride string, runtimeParams map[string]interface{}, hub *sessionHub, agentCtx context.Context) {
	log.Printf("[app-run:%s] Dispatching %s run for app %s", run.ID[:8], run.Trigger, run.AppID[:8])

	// Clean up the session hub on any exit path (success, failure, panic).
	// The hub was registered synchronously in DispatchRun to avoid a race
	// with the HTTP client's /stream resume — see the note there.
	defer func() {
		if hub != nil {
			as.server.removeSessionHub(run.SessionID)
			hub.close()
		}
	}()

	// Check monthly run quota
	if quotaStr, err := as.server.db.GetSetting("run_quota_monthly", run.UserID); err == nil && quotaStr != "" {
		var quota int
		fmt.Sscanf(quotaStr, "%d", &quota)
		if quota > 0 {
			used, _ := as.server.db.CountMonthlyRuns(run.UserID)
			if used >= quota {
				log.Printf("[app-run:%s] Monthly quota exceeded (%d/%d) for user %s", run.ID[:8], used, quota, run.UserID)
				as.server.db.UpdateAppRunResult(run.ID, "failed", "", fmt.Sprintf("Error: Monthly run quota exceeded (%d/%d). Upgrade your plan for more runs.", used, quota))
				return
			}
		}
	}

	app, err := as.server.db.GetApp(run.AppID)
	if err != nil {
		log.Printf("[app-run:%s] App %s not found: %v", run.ID[:8], run.AppID[:8], err)
		as.server.db.UpdateAppRunStatus(run.ID, "failed")
		return
	}

	prompt := apps.ResolveWithOverrides(app.Prompt, app.Parameters, app.ParameterDefs, runtimeParams)
	if promptOverride != "" {
		prompt = promptOverride
	}

	// Memory is now pull-only via the tofi_recall_memory tool. We used to
	// auto-prepend "## Context from Previous Runs" with the top-5 recalled
	// rows to every prompt, but that leaked unrelated other-agent context
	// (site-health-check output bleeding into a VOO price query, etc.)
	// straight into the user-visible message bubble — making the product
	// look hallucinatory. Agents that want memory now have to ask for it,
	// which is the same contract they already follow for every other tool.

	// Create a Chat Session for this app run. Use the session ID pre-allocated
	// in DispatchRun so the HTTP caller's navigation target stays stable.
	scope := chat.AgentScope("app-" + app.ID[:8])
	sessionID := run.SessionID
	if sessionID == "" {
		sessionID = "s_" + uuid.New().String()[:12]
	}

	// Build skills string from app config
	var skillNames []string
	json.Unmarshal([]byte(app.Skills), &skillNames)
	skillsStr := strings.Join(skillNames, ",")

	session := chat.NewSession(sessionID, app.Model, skillsStr)
	session.Title = fmt.Sprintf("[App: %s] %s", app.Name, app.Description)

	if err := as.server.chatStore.Save(run.UserID, scope, session); err != nil {
		log.Printf("[app-run:%s] Failed to create chat session: %v", run.ID[:8], err)
		as.server.db.UpdateAppRunStatus(run.ID, "failed")
		return
	}

	// Link run to session
	as.server.db.UpdateAppRunStatusWithSession(run.ID, "running", sessionID)

	// Forward every onEvent from the agent loop through the session hub
	// registered in DispatchRun. The hub itself is cleaned up by our
	// top-level defer; we just publish here.
	onEvent := func(eventType string, data any) {
		if hub != nil {
			hub.publish(eventType, data)
		}
	}

	log.Printf("[app-run:%s] Executing with chat session %s", run.ID[:8], sessionID[:8])
	result, err := as.server.executeChatSession(run.UserID, scope, session, prompt, onEvent, &bridge.ExecuteOptions{Ctx: agentCtx}, run.AppID)

	status := "done"
	var failReason string
	if err != nil {
		log.Printf("[app-run:%s] Chat session execution failed: %v", run.ID[:8], err)
		// Distinguish "the agent hit tofi_ask_user and couldn't get an
		// answer" from genuine execution failures — the former shouldn't
		// pollute success-rate stats or look like a crash in run history.
		// Error strings come from chat_handlers.go's AskUserFn closure.
		msg := err.Error()
		switch {
		case strings.Contains(msg, "did not respond within"):
			status = "timed_out"
		case strings.Contains(msg, "user declined to answer"):
			status = "aborted"
		default:
			status = "failed"
		}
		failReason = msg
	} else {
		log.Printf("[app-run:%s] Completed (tokens: %d in / %d out, cost: $%.4f)",
			run.ID[:8], result.TotalUsage.InputTokens, result.TotalUsage.OutputTokens, result.TotalCost)

		// Auto-notify: send AI output to configured notify targets
		if result.Content != "" {
			sent, notifyErr := connect.SendNotification(run.UserID, app.ID, result.Content, connect.NotifyDeps{
				ListConnectorsLinkedToApp: as.server.db.ListConnectorsLinkedToApp,
				ListConnectorsForApp:      as.server.db.ListConnectorsForApp,
				ListConnectors:            as.server.db.ListConnectors,
				ListConnectorReceivers:    as.server.db.ListConnectorReceivers,
			})
			if notifyErr != nil {
				log.Printf("[app-run:%s] Notification error: %v", run.ID[:8], notifyErr)
			} else if sent > 0 {
				log.Printf("[app-run:%s] Auto-notified %d receiver(s)", run.ID[:8], sent)
			} else {
				log.Printf("[app-run:%s] No receivers found for notifications", run.ID[:8])
			}
		} else {
			log.Printf("[app-run:%s] No content to notify (empty result)", run.ID[:8])
		}
	}
	// Save result content to DB for historical queries
	resultContent := ""
	if result != nil && result.Content != "" {
		resultContent = result.Content
	} else if failReason != "" {
		resultContent = "Error: " + failReason
	}
	as.server.db.UpdateAppRunResult(run.ID, status, sessionID, resultContent)

	// Auto-save run summary to memory for future runs
	if status == "done" && resultContent != "" {
		summary := fmt.Sprintf("[App: %s, Run: %s] Completed at %s.",
			app.Name, run.ID[:8], time.Now().Format("2006-01-02 15:04"))
		if len(resultContent) > 200 {
			summary += " Output preview: " + resultContent[:200] + "..."
		} else {
			summary += " Output: " + resultContent
		}
		if _, err := as.server.db.SaveMemory(run.UserID, summary, "app-run,"+app.ID, "system", ""); err != nil {
			log.Printf("[app-run:%s] Failed to save memory: %v", run.ID[:8], err)
		}
	}

	// Write execution log file
	as.writeRunLog(run, app, result, status, sessionID)
}

func (as *AppScheduler) checkRenewals() {
	activeApps, err := as.server.db.ListActiveApps()
	if err != nil {
		log.Printf("[app-scheduler] Failed to list active apps: %v", err)
		return
	}

	for _, app := range activeApps {
		count, err := as.server.db.CountPendingAppRuns(app.ID)
		if err != nil {
			continue
		}
		if count < app.RenewalThreshold {
			as.doRenewal(app)
		}
	}
}

func (as *AppScheduler) doRenewal(app *storage.AppRecord) {
	count, err := as.server.db.CountPendingAppRuns(app.ID)
	if err != nil {
		return
	}
	need := app.BufferSize - count
	if need <= 0 {
		return
	}

	log.Printf("[app:%s] Renewal: %d pending, need %d more", app.ID[:8], count, need)

	lastTime, _ := as.server.db.GetLastAppScheduledTime(app.ID)
	times := ExpandSchedule(app.ScheduleRules, lastTime, need)
	if len(times) == 0 {
		return
	}

	added := 0
	for _, t := range times {
		run := &storage.AppRunRecord{
			ID:          uuid.New().String(),
			AppID:       app.ID,
			ScheduledAt: t.UTC().Format("2006-01-02 15:04:05"),
			Status:      "pending",
			UserID:      app.UserID,
		}
		if err := as.server.db.CreateAppRun(run); err != nil {
			// Stop at first failure to prevent gaps: if we continue past a failed
			// time slot, GetLastAppScheduledTime will skip over it permanently.
			log.Printf("[app:%s] Renewal stopped at %s: %v", app.ID[:8], t.Format("15:04"), err)
			break
		}
		added++
	}

	if added > 0 {
		log.Printf("[app:%s] Renewal complete: added %d runs", app.ID[:8], added)
	}
}

// ── Public methods ──

func (as *AppScheduler) ActivateApp(app *storage.AppRecord) error {
	startFrom := time.Now()
	times := ExpandSchedule(app.ScheduleRules, startFrom, app.BufferSize)
	if len(times) == 0 {
		return fmt.Errorf("schedule rules produced no future runs")
	}

	for _, t := range times {
		run := &storage.AppRunRecord{
			ID:          uuid.New().String(),
			AppID:       app.ID,
			ScheduledAt: t.UTC().Format("2006-01-02 15:04:05"),
			Status:      "pending",
			UserID:      app.UserID,
		}
		if err := as.server.db.CreateAppRun(run); err != nil {
			continue
		}
	}

	log.Printf("App %s activated with %d scheduled runs", app.ID[:8], len(times))
	return nil
}

func (as *AppScheduler) RemoveApp(appID string) {
	// No-op: deactivation already calls CancelPendingAppRuns via handler.
	// DB-poll model doesn't need in-memory cleanup.
}

// ── Schedule Expansion ──

// ExpandSchedule detects v2 (entries) or v1 (rules) format and dispatches accordingly.
func ExpandSchedule(rulesJSON string, startFrom time.Time, count int) []time.Time {
	if count <= 0 {
		return nil
	}

	// Try v2 format first (has "entries" key)
	var v2 Schedule
	if err := json.Unmarshal([]byte(rulesJSON), &v2); err == nil && len(v2.Entries) > 0 {
		return expandEntries(v2, startFrom, count)
	}

	// Fall back to v1 format (has "rules" key)
	return expandLegacyRules(rulesJSON, startFrom, count)
}

// expandEntries handles v2 entry-based alarm model
func expandEntries(schedule Schedule, startFrom time.Time, count int) []time.Time {
	loc := time.UTC
	if schedule.Timezone != "" {
		if l, err := time.LoadLocation(schedule.Timezone); err == nil {
			loc = l
		}
	}

	var results []time.Time
	cursor := startFrom.In(loc).Truncate(time.Minute).Add(time.Minute)
	maxDate := cursor.Add(365 * 24 * time.Hour)

	for cursor.Before(maxDate) && len(results) < count {
		for _, entry := range schedule.Entries {
			if !entry.Enabled {
				continue
			}
			if !entryMatchesDate(entry, cursor) {
				continue
			}

			startH, startM := parseHHMM(entry.Time)
			startTime := time.Date(cursor.Year(), cursor.Month(), cursor.Day(), startH, startM, 0, 0, loc)

			if entry.IntervalMin > 0 && entry.EndTime != "" {
				// Interval window
				endH, endM := parseHHMM(entry.EndTime)
				endTime := time.Date(cursor.Year(), cursor.Month(), cursor.Day(), endH, endM, 0, 0, loc)
				if endTime.Before(startTime) {
					endTime = startTime
				}
				interval := time.Duration(entry.IntervalMin) * time.Minute
				t := startTime
				for !t.After(endTime) && len(results) < count {
					if t.After(startFrom) {
						results = append(results, t)
					}
					t = t.Add(interval)
				}
			} else {
				// Single run at time
				if startTime.After(startFrom) && len(results) < count {
					results = append(results, startTime)
				}
			}
		}
		cursor = time.Date(cursor.Year(), cursor.Month(), cursor.Day()+1, 0, 0, 0, 0, loc)
	}

	return results
}

func entryMatchesDate(entry ScheduleEntry, date time.Time) bool {
	switch entry.Repeat.Type {
	case "daily":
		return true
	case "weekly":
		return weekdayMatches(entry.Repeat.Days, date.Weekday())
	case "monthly":
		day := date.Day()
		for _, d := range entry.Repeat.Dates {
			if d == day {
				return true
			}
		}
		return false
	case "once":
		if entry.Repeat.Date == "" {
			return false
		}
		target, err := time.Parse("2006-01-02", entry.Repeat.Date)
		if err != nil {
			return false
		}
		return date.Year() == target.Year() && date.Month() == target.Month() && date.Day() == target.Day()
	default:
		return false
	}
}

var dayMap = map[string]time.Weekday{
	"mon": time.Monday, "tue": time.Tuesday, "wed": time.Wednesday,
	"thu": time.Thursday, "fri": time.Friday, "sat": time.Saturday, "sun": time.Sunday,
}

func weekdayMatches(days []string, weekday time.Weekday) bool {
	if len(days) == 0 {
		return true
	}
	for _, d := range days {
		if mapped, ok := dayMap[strings.ToLower(d)]; ok && mapped == weekday {
			return true
		}
	}
	return false
}

// expandLegacyRules handles v1 rules-based format
func expandLegacyRules(rulesJSON string, startFrom time.Time, count int) []time.Time {
	var schedule ScheduleRule
	if err := json.Unmarshal([]byte(rulesJSON), &schedule); err != nil {
		log.Printf("Failed to parse schedule rules: %v", err)
		return nil
	}
	if len(schedule.Rules) == 0 {
		return nil
	}

	loc := time.UTC
	if schedule.Timezone != "" {
		if l, err := time.LoadLocation(schedule.Timezone); err == nil {
			loc = l
		}
	}

	var results []time.Time
	cursor := startFrom.In(loc).Truncate(time.Minute).Add(time.Minute)
	maxDate := cursor.Add(365 * 24 * time.Hour)

	for cursor.Before(maxDate) && len(results) < count {
		weekday := cursor.Weekday()

		for _, rule := range schedule.Rules {
			if !weekdayMatches(rule.Days, weekday) {
				continue
			}

			for _, win := range rule.Windows {
				startH, startM := parseHHMM(win.Start)
				endH, endM := parseHHMM(win.End)

				startTime := time.Date(cursor.Year(), cursor.Month(), cursor.Day(), startH, startM, 0, 0, loc)
				endTime := time.Date(cursor.Year(), cursor.Month(), cursor.Day(), endH, endM, 0, 0, loc)

				if endTime.Before(startTime) {
					endTime = startTime
				}

				interval := time.Duration(win.IntervalMin) * time.Minute
				if interval <= 0 {
					if startTime.After(startFrom) && len(results) < count {
						results = append(results, startTime)
					}
				} else {
					t := startTime
					for !t.After(endTime) && len(results) < count {
						if t.After(startFrom) {
							results = append(results, t)
						}
						t = t.Add(interval)
					}
				}
			}
		}

		cursor = time.Date(cursor.Year(), cursor.Month(), cursor.Day()+1, 0, 0, 0, 0, loc)
	}

	return results
}

func parseHHMM(s string) (int, int) {
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return 0, 0
	}
	h, m := 0, 0
	fmt.Sscanf(parts[0], "%d", &h)
	fmt.Sscanf(parts[1], "%d", &m)
	return h, m
}

// ── Run Logs ──

// writeRunLog saves a structured log file for each app run.
func (as *AppScheduler) writeRunLog(run *storage.AppRunRecord, app *storage.AppRecord, result *agent.AgentResult, status, sessionID string) {
	logsDir := filepath.Join(as.server.config.HomeDir, "users", run.UserID, "agents", app.ID, "logs")
	os.MkdirAll(logsDir, 0755)

	logPath := filepath.Join(logsDir, run.ID[:8]+".log")

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("=== App Run Log ===\n"))
	sb.WriteString(fmt.Sprintf("Run ID:     %s\n", run.ID))
	sb.WriteString(fmt.Sprintf("App:        %s (%s)\n", app.Name, app.ID))
	sb.WriteString(fmt.Sprintf("Status:     %s\n", status))
	sb.WriteString(fmt.Sprintf("Trigger:    %s\n", run.Trigger))
	sb.WriteString(fmt.Sprintf("Session:    %s\n", sessionID))
	sb.WriteString(fmt.Sprintf("Started:    %s\n", time.Now().Format(time.RFC3339)))

	if result != nil {
		sb.WriteString(fmt.Sprintf("Tokens In:  %d\n", result.TotalUsage.InputTokens))
		sb.WriteString(fmt.Sprintf("Tokens Out: %d\n", result.TotalUsage.OutputTokens))
		sb.WriteString(fmt.Sprintf("Cost:       $%.4f\n", result.TotalCost))
		sb.WriteString(fmt.Sprintf("Model:      %s\n", result.Model))
	}

	sb.WriteString(fmt.Sprintf("\n=== Output ===\n"))
	if result != nil && result.Content != "" {
		sb.WriteString(result.Content)
	} else {
		sb.WriteString("(no output)")
	}
	sb.WriteString("\n")

	if err := os.WriteFile(logPath, []byte(sb.String()), 0644); err != nil {
		log.Printf("[app-run:%s] Failed to write log: %v", run.ID[:8], err)
	}
}

// ── TTL Cleanup ──

const (
	appRunLogRetention     = 7 * 24 * time.Hour  // 7 days for logs
	appRunSessionRetention = 30 * 24 * time.Hour // 30 days for session XMLs
)

// startAppRunCleanup launches a goroutine to clean expired logs and sessions.
func (s *Server) startAppRunCleanup() {
	go func() {
		// Run once on startup after a short delay
		time.Sleep(30 * time.Second)
		s.cleanAppRunFiles()

		ticker := time.NewTicker(6 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			s.cleanAppRunFiles()
		}
	}()
}

func (s *Server) cleanAppRunFiles() {
	usersDir := filepath.Join(s.config.HomeDir, "users")
	userEntries, err := os.ReadDir(usersDir)
	if err != nil {
		return
	}

	now := time.Now()
	logsRemoved, xmlRemoved := 0, 0

	for _, ue := range userEntries {
		if !ue.IsDir() {
			continue
		}
		agentsDir := filepath.Join(usersDir, ue.Name(), "agents")
		agentEntries, err := os.ReadDir(agentsDir)
		if err != nil {
			continue
		}

		for _, ae := range agentEntries {
			if !ae.IsDir() {
				continue
			}

			// Clean logs
			logsDir := filepath.Join(agentsDir, ae.Name(), "logs")
			logsRemoved += cleanOldFiles(logsDir, ".log", now, appRunLogRetention)

			// Clean session XMLs
			chatDir := filepath.Join(agentsDir, ae.Name(), "chat")
			xmlRemoved += cleanOldFiles(chatDir, ".xml", now, appRunSessionRetention)
		}
	}

	if logsRemoved > 0 || xmlRemoved > 0 {
		log.Printf("[cleanup] Removed %d expired logs, %d expired session XMLs", logsRemoved, xmlRemoved)
	}
}

// cleanOldFiles removes files with the given suffix that are older than maxAge.
func cleanOldFiles(dir, suffix string, now time.Time, maxAge time.Duration) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}

	removed := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), suffix) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if now.Sub(info.ModTime()) > maxAge {
			os.Remove(filepath.Join(dir, e.Name()))
			removed++
		}
	}
	return removed
}

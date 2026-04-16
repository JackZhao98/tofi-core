package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"tofi-core/internal/bridge"
	"tofi-core/internal/chat"
	"tofi-core/internal/connect"
	"tofi-core/internal/executor"
	"tofi-core/internal/agent"
	"tofi-core/internal/models"
	"tofi-core/internal/provider"
	"tofi-core/internal/skills"
	"tofi-core/internal/storage"

	"github.com/google/uuid"
)

// --- POST /api/v1/chat/sessions ---

func (s *Server) handleCreateChatSession(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(UserContextKey).(string)

	var req struct {
		Scope  string   `json:"scope"`  // "" or "agent:{name}"
		Model  string   `json:"model"`
		Skills []string `json:"skills"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// Allow empty body — all fields optional
		req = struct {
			Scope  string   `json:"scope"`
			Model  string   `json:"model"`
			Skills []string `json:"skills"`
		}{}
	}

	skillsStr := strings.Join(req.Skills, ",")
	sessionID := "s_" + uuid.New().String()[:12]
	session := chat.NewSession(sessionID, req.Model, skillsStr)

	if err := s.chatStore.Save(userID, req.Scope, session); err != nil {
		writeJSONError(w, http.StatusInternalServerError, ErrInternal, "failed to create session: "+err.Error(), "")
		return
	}

	// Resolve the effective model for the response
	effectiveModel := session.Model
	var warning string
	resolvedModel, _, _, resolveErr := s.resolveModelAndKey(userID, session.Model)
	if resolveErr != nil {
		// No key configured — session created but warn the user
		if keyErr, ok := resolveErr.(*apiKeyError); ok {
			warning = keyErr.Message
			effectiveModel = ""
		}
	} else {
		effectiveModel = resolvedModel
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	resp := map[string]any{
		"id":      session.ID,
		"scope":   req.Scope,
		"model":   effectiveModel,
		"skills":  req.Skills,
		"created": session.Created,
	}
	if warning != "" {
		resp["warning"] = warning
		resp["hint"] = "PUT /api/v1/user/settings/ai-key with {\"provider\": \"openai\", \"api_key\": \"sk-...\"}"
	}
	json.NewEncoder(w).Encode(resp)
}

// --- GET /api/v1/chat/sessions ---

func (s *Server) handleListChatSessions(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(UserContextKey).(string)
	scope := r.URL.Query().Get("scope")
	// If scope param is absent entirely, list all scopes.
	// If scope param is present but empty (?scope=), filter by empty scope (user main chat).
	if !r.URL.Query().Has("scope") {
		scope = "*"
	}

	sessions, err := s.chatStore.List(userID, scope, 50)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, ErrInternal, err.Error(), "")
		return
	}

	// Ensure we return [] not null for empty results
	if sessions == nil {
		sessions = []*storage.ChatSessionIndex{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sessions)
}

// --- GET /api/v1/chat/sessions/{id} ---

func (s *Server) handleGetChatSession(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(UserContextKey).(string)
	sessionID := r.PathValue("id")

	// Ownership check via index
	idx, err := s.chatStore.GetIndex(sessionID)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, ErrSessionNotFound, "session not found", "")
		return
	}
	if idx.UserID != userID {
		writeJSONError(w, http.StatusForbidden, ErrForbidden, "forbidden", "")
		return
	}

	session, err := s.chatStore.LoadByID(sessionID)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, ErrSessionNotFound, "session not found: "+err.Error(), "")
		return
	}

	response := struct {
		*chat.Session
		ContextUsagePercent int `json:"context_usage_percent"`
	}{
		Session:             session,
		ContextUsagePercent: chat.ContextUsagePercent(session.Usage.InputTokens, session.Model),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// --- GET /api/v1/chat/sessions/{id}/capabilities ---
//
// Returns three lists for the session info panel:
//   - builtin_tools:    always-available agent tools (no need to load)
//   - available_skills: skills the agent can call tofi_load_skill on
//   - loaded_skills:    skills already activated in this session (Session.LoadedSkills)
//
// Built-in tool names are kept in sync with what chat_handlers.go wires up
// for chat sessions (buildBuiltinTools + memory + schedule + sub-agent etc).
// They're enumerated here so the frontend can render them in the info panel
// without having to mirror the agent harness internals.
func (s *Server) handleGetChatSessionCapabilities(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(UserContextKey).(string)
	sessionID := r.PathValue("id")

	idx, err := s.chatStore.GetIndex(sessionID)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, ErrSessionNotFound, "session not found", "")
		return
	}
	if idx.UserID != userID {
		writeJSONError(w, http.StatusForbidden, ErrForbidden, "forbidden", "")
		return
	}
	session, err := s.chatStore.LoadByID(sessionID)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, ErrSessionNotFound, "session not found", "")
		return
	}

	type toolInfo struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	type skillInfo struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Scope       string `json:"scope"`
	}

	// Built-in tools — same set the agent loop registers for chat sessions.
	// Names match what's exposed to the model so the user can correlate
	// tool calls in the transcript with this list.
	builtin := []toolInfo{
		{"tofi_load_skill", "Load the full instructions for a skill on demand"},
		{"tofi_sub_agent", "Spawn a fresh sub-agent in an isolated context"},
		{"tofi_get_time", "Get current date, time, and timezone"},
		{"tofi_get_user", "Get the current user's identity"},
		{"tofi_session_info", "Get token usage, cost, and session statistics"},
		{"tofi_memory_read", "Read persistent memory entries for the user"},
		{"tofi_memory_write", "Write a persistent memory entry for the user"},
		{"tofi_memory_search", "Search persistent memory entries"},
		{"tofi_schedule_create", "Schedule a task to run later"},
		{"tofi_schedule_list", "List scheduled tasks"},
		{"tofi_schedule_cancel", "Cancel a scheduled task"},
		{"tofi_task_list", "List background tasks"},
		{"tofi_task_stop", "Stop a running background task"},
		{"tofi_notify", "Send a message via configured notification channels"},
		{"tofi_skill_install", "Install a new skill from a source URL or zip"},
		{"tofi_wish_grant", "Honor a /wish slash command result"},
	}

	// Available skills: system embed FS + this user's filesystem.
	availableSet := make(map[string]skillInfo)
	for name, sf := range skills.LoadAllSystemSkills() {
		availableSet[name] = skillInfo{
			Name:        name,
			Description: sf.Manifest.Description,
			Scope:       "system",
		}
	}
	localStore := skills.NewLocalStore(s.config.HomeDir)
	if userSkills, _ := localStore.ListUserSkills(userID); userSkills != nil {
		for _, sk := range userSkills {
			availableSet[sk.Name] = skillInfo{
				Name:        sk.Name,
				Description: sk.Desc,
				Scope:       "user",
			}
		}
	}
	available := make([]skillInfo, 0, len(availableSet))
	for _, v := range availableSet {
		available = append(available, v)
	}
	sort.Slice(available, func(i, j int) bool { return available[i].Name < available[j].Name })

	// Loaded skills for this session (Session.LoadedSkills, comma-separated).
	var loaded []string
	if session.LoadedSkills != "" {
		for _, n := range strings.Split(session.LoadedSkills, ",") {
			n = strings.TrimSpace(n)
			if n != "" {
				loaded = append(loaded, n)
			}
		}
	}
	if loaded == nil {
		loaded = []string{}
	}

	resp := map[string]interface{}{
		"builtin_tools":    builtin,
		"available_skills": available,
		"loaded_skills":    loaded,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// --- DELETE /api/v1/chat/sessions/{id} ---

func (s *Server) handleDeleteChatSession(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(UserContextKey).(string)
	sessionID := r.PathValue("id")

	idx, err := s.chatStore.GetIndex(sessionID)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, ErrSessionNotFound, "session not found", "")
		return
	}
	if idx.UserID != userID {
		writeJSONError(w, http.StatusForbidden, ErrForbidden, "forbidden", "")
		return
	}

	if err := s.chatStore.Delete(userID, idx.Scope, sessionID); err != nil {
		writeJSONError(w, http.StatusInternalServerError, ErrInternal, err.Error(), "")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- PATCH /api/v1/chat/sessions/{id} ---

func (s *Server) handleUpdateChatSession(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(UserContextKey).(string)
	sessionID := r.PathValue("id")

	idx, err := s.chatStore.GetIndex(sessionID)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, ErrSessionNotFound, "session not found", "")
		return
	}
	if idx.UserID != userID {
		writeJSONError(w, http.StatusForbidden, ErrForbidden, "forbidden", "")
		return
	}

	session, err := s.chatStore.LoadByID(sessionID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, ErrInternal, "failed to load session: "+err.Error(), "")
		return
	}

	var req struct {
		Model  *string  `json:"model"`
		Skills []string `json:"skills"`
		Title  *string  `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, ErrBadRequest, "invalid request", "")
		return
	}

	if req.Model != nil {
		session.Model = *req.Model
	}
	if req.Skills != nil {
		session.Skills = strings.Join(req.Skills, ",")
	}
	if req.Title != nil {
		session.Title = *req.Title
	}
	session.Updated = time.Now().UTC().Format(time.RFC3339)

	if err := s.chatStore.Save(userID, idx.Scope, session); err != nil {
		writeJSONError(w, http.StatusInternalServerError, ErrInternal, err.Error(), "")
		return
	}

	var skillsList []string
	if session.Skills != "" {
		skillsList = strings.Split(session.Skills, ",")
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"id":     session.ID,
		"model":  session.Model,
		"skills": skillsList,
		"title":  session.Title,
	})
}

// --- POST /api/v1/chat/sessions/{id}/messages ---

func (s *Server) handleChatMessage(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(UserContextKey).(string)
	sessionID := r.PathValue("id")

	var req struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Message == "" {
		writeJSONError(w, http.StatusBadRequest, ErrBadRequest, "message is required", "")
		return
	}

	// 1. Load session index for ownership check + scope
	idx, err := s.chatStore.GetIndex(sessionID)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, ErrSessionNotFound, "session not found", "")
		return
	}
	if idx.UserID != userID {
		writeJSONError(w, http.StatusForbidden, ErrForbidden, "forbidden", "")
		return
	}

	// 2. Load full session from XML
	session, err := s.chatStore.LoadByID(sessionID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, ErrInternal, "failed to load session: "+err.Error(), "")
		return
	}

	// 3. Agent runs on an independent context so it survives HTTP client
	// disconnects (e.g. browser refresh). Cancellation is driven exclusively
	// by POST /abort (which also signals any active hold).
	agentCtx, cancel := context.WithCancel(context.Background())

	// 4. Register the session hub. This also enforces a one-agent-per-session
	// invariant: a second POST /messages while an agent is still running
	// (e.g. paused on hold) returns 409.
	hub, err := s.createSessionHub(sessionID, cancel)
	if err != nil {
		cancel()
		writeJSONError(w, http.StatusConflict, ErrConflict, "session already has an active agent; use /continue or /abort", "")
		return
	}

	// 5. Kick off the agent in a goroutine; HTTP lifecycle no longer gates it.
	go func() {
		defer s.removeSessionHub(sessionID)
		defer hub.close()
		defer func() {
			// The HTTP mux recovers sync handlers, but this goroutine runs
			// outside that stack. Catch panics here so a bad tool or skill
			// can't crash the whole server.
			if r := recover(); r != nil {
				log.Printf("🔥 [chat:%s] agent goroutine panic: %v", sessionID[:8], r)
				// Restore session to a clean state so the UI doesn't leave
				// the user stuck on a "running" or "hold" badge forever.
				session.Status = ""
				session.HoldInfo = nil
				_ = s.chatStore.Save(userID, idx.Scope, session)
				hub.publish("error", map[string]string{
					"code":  ErrInternal,
					"error": "agent crashed unexpectedly",
				})
			}
		}()

		onEvent := func(eventType string, data any) {
			hub.publish(eventType, data)
		}

		result, runErr := s.executeChatSession(userID, idx.Scope, session, req.Message, onEvent, &bridge.ExecuteOptions{Ctx: agentCtx})
		if runErr != nil {
			// apiKeyError already emitted a structured error event via onEvent.
			if _, ok := runErr.(*apiKeyError); !ok {
				errMsg, errCode, errHint := classifyAgentError(runErr)
				hub.publish("error", map[string]string{
					"code":  errCode,
					"error": errMsg,
					"hint":  errHint,
				})
			}
			return
		}

		contextPct := chat.ContextUsagePercent(result.TotalUsage.InputTokens, result.Model)
		hub.publish("done", map[string]any{
			"result":                result.Content,
			"model":                 result.Model,
			"total_input_tokens":    result.TotalUsage.InputTokens,
			"total_output_tokens":   result.TotalUsage.OutputTokens,
			"total_cost":            result.TotalCost,
			"llm_calls":             result.LLMCalls,
			"context_usage_percent": contextPct,
		})
	}()

	// 6. Subscribe and forward hub events to this HTTP response.
	streamSessionEventsToSSE(w, r, hub, sessionID, 0)
}

// streamSessionEventsToSSE subscribes to a session hub and forwards every
// event to the HTTP client until the hub closes (agent done) or the client
// disconnects. fromSeq lets resumers replay from a known checkpoint.
func streamSessionEventsToSSE(w http.ResponseWriter, r *http.Request, hub *sessionHub, sessionID string, fromSeq int) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	rc := http.NewResponseController(w)
	rc.SetWriteDeadline(time.Time{})
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, ErrInternal, "streaming not supported", "")
		return
	}

	connData, _ := json.Marshal(map[string]string{"session_id": sessionID})
	fmt.Fprintf(w, "event: connected\ndata: %s\n\n", connData)
	flusher.Flush()

	subID, ch := hub.subscribe(fromSeq)
	defer hub.unsubscribe(subID)

	forwardHubToSSE(w, flusher, ch, r.Context().Done())
}

// executeChatSession runs an agent loop for a chat session.
// Used by both the HTTP handler (SSE) and the app scheduler.
// The onEvent callback receives events: "chunk", "tool_call", "context_compact", "error".
// If onEvent is nil, events are silently discarded.
func (s *Server) executeChatSession(userID, scope string, session *chat.Session, message string, onEvent func(eventType string, data any), opts *bridge.ExecuteOptions) (*agent.AgentResult, error) {
	sessionID := session.ID

	emit := func(eventType string, data any) {
		if onEvent != nil {
			onEvent(eventType, data)
		}
	}

	// 1. Resolve model and API key
	resolvedModel, apiKey, _, err := s.resolveModelAndKey(userID, session.Model)
	if err != nil {
		// Return structured error for AI key issues so callers can produce actionable messages
		if keyErr, ok := err.(*apiKeyError); ok {
			emit("error", map[string]string{
				"code":    keyErr.Code,
				"error":   keyErr.Message,
				"hint":    keyErr.Hint,
			})
			return nil, keyErr
		}
		return nil, fmt.Errorf("model resolution failed: %w", err)
	}

	// 1.5 Check spend cap
	if err := s.checkSpendCap(userID); err != nil {
		emit("error", map[string]string{
			"code":  ErrSpendCapExceeded,
			"error": err.Error(),
		})
		return nil, fmt.Errorf("spend cap: %w", err)
	}

	// 2. Build system prompt based on scope
	systemPrompt := s.buildChatSystemPrompt(userID, scope)

	// 3. Load skills — filesystem is the single source of truth.
	// System skills from embed FS, user skills from filesystem.
	// All skills are deferred: name+description in system prompt,
	// full Instructions loaded on-demand via tofi_load_skill tool.
	skillNames := skills.ListSystemSkillNames()

	// Global chat: auto-import all user-installed skills
	if !strings.HasPrefix(scope, chat.ScopeAgentPrefix) {
		localStore := skills.NewLocalStore(s.config.HomeDir)
		if userSkills, err := localStore.ListUserSkills(userID); err == nil {
			for _, sk := range userSkills {
				skillNames = append(skillNames, sk.Name)
			}
		}
	}

	// App/agent scope: add session-specified skills
	if session.Skills != "" {
		for _, name := range strings.Split(session.Skills, ",") {
			name = strings.TrimSpace(name)
			if name != "" {
				skillNames = append(skillNames, name)
			}
		}
	}

	// Deduplicate
	seen := make(map[string]bool)
	deduped := skillNames[:0]
	for _, name := range skillNames {
		if !seen[name] {
			seen[name] = true
			deduped = append(deduped, name)
		}
	}
	skillNames = deduped
	skillTools, _, secretEnv := s.buildSkillTools(userID, skillNames)

	// 4. Build provider messages from session history
	providerMessages := chat.BuildProviderMessages(session, message, resolvedModel)

	// 5. Parse capabilities for agent scope
	var capMCPServers []agent.MCPServerConfig
	var extraTools []agent.ExtraBuiltinTool
	if agentName, ok := strings.CutPrefix(scope, chat.ScopeAgentPrefix); ok {
		if agentDef, err := s.workspace.ReadAgent(userID, agentName); err == nil {
			if agentDef.Config.Capabilities != nil {
				capMCPServers, extraTools = s.buildCapabilitiesFromMap(userID, agentDef.Config.Capabilities)
			}
		}
	}

	// 6. Create sandbox
	sandboxDir, err := s.executor.CreateSandbox(executor.SandboxConfig{
		HomeDir: s.config.HomeDir,
		UserID:  userID,
		CardID:  "chat-" + sessionID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create sandbox: %w", err)
	}
	defer s.executor.Cleanup(sandboxDir)

	// 7. Persist user message and mark session as running.
	// User message is saved BEFORE the agent loop so that if the loop
	// pauses (hold/ask_user) or the client disconnects, a subsequent
	// GET /chat/sessions/{id} still returns this turn's prompt.
	session.AddMessage(chat.Message{
		Role:    "user",
		Content: message,
	})
	session.Status = "running"
	if err := s.chatStore.Save(userID, scope, session); err != nil {
		log.Printf("⚠️  [chat:%s] failed to save running state: %v", sessionID[:8], err)
	}

	// 8. Build AgentConfig
	var liveUsage provider.Usage // real-time usage updated during agent loop

	// Core tools: always available in every chat session
	coreTools := make([]agent.ExtraBuiltinTool, len(extraTools))
	copy(coreTools, extraTools)
	// App webhook runs must not pause for user input; used below to gate
	// tools that expect an interactive client.
	isAppRun := strings.HasPrefix(scope, chat.ScopeAgentPrefix+"app-")
	// Marketplace search is only exposed in the user's free-form chat.
	// Agents and Apps are pre-configured by their creator; letting the agent
	// autonomously pull new skills from the marketplace causes two problems:
	//   1) it ignores skills already wired into the agent and re-searches
	//      the same capability (observed in v0.8.0 with web-search);
	//   2) there is no real install path — the old tofi_suggest_install
	//      hard-coded a "successfully installed" reply, so the agent then
	//      tried to use a skill that was never on disk.
	if scope == chat.ScopeUser {
		coreTools = append(coreTools, s.buildChatWishTools()...)
	}
	coreTools = append(coreTools, s.buildMemoryTools(userID, "")...)
	coreTools = append(coreTools, s.buildBuiltinTools(userID)...)
	coreTools = append(coreTools, buildSessionInfoTool(session, resolvedModel, &liveUsage))
	coreTools = append(coreTools, s.buildScheduleTools(userID)...)

	// Bundle app tools with ALL app-related skills
	// Loading any app skill (apps, app-create, app-list, etc.) activates the tools
	appTools := s.buildAppTools(userID)
	for i := range skillTools {
		name := skillTools[i].Name
		if name == "apps" || strings.HasPrefix(name, "app-") {
			skillTools[i].BundledTools = append(skillTools[i].BundledTools, appTools...)
		}
	}

	// Bundle tofi_notify with a notify skill (for interactive chat)
	// App runs don't need this — the runtime auto-sends notifications after completion
	if !isAppRun {
		notifyAppID := ""
		if agentName, ok := strings.CutPrefix(scope, chat.ScopeAgentPrefix); ok {
			if appIDPrefix, ok := strings.CutPrefix(agentName, "app-"); ok {
				if app, err := s.db.GetApp(appIDPrefix); err == nil {
					notifyAppID = app.ID
				} else {
					apps, _ := s.db.ListApps(userID)
					for _, a := range apps {
						if strings.HasPrefix(a.ID, appIDPrefix) {
							notifyAppID = a.ID
							break
						}
					}
				}
			}
		}
		notifyDeps := connect.NotifyDeps{
			ListConnectorsForApp:   s.db.ListConnectorsForApp,
			ListConnectors:         s.db.ListConnectors,
			ListConnectorReceivers: s.db.ListConnectorReceivers,
		}
		notifyTools := connect.InjectNotifyTool(nil, userID, notifyAppID, notifyDeps)
		// Bundle with a "notify" skill entry
		skillTools = append(skillTools, agent.SkillTool{
			Name:         "notify",
			Description:  "Send notifications to configured channels (Telegram, Slack, Discord, Email)",
			Instructions: "Use the tofi_notify tool to send a message to the user's configured notification channels.",
			BundledTools: notifyTools,
		})
	}

	// Parse previously loaded skills from session for pre-activation
	var preloadedSkills []string
	if session.LoadedSkills != "" {
		for _, name := range strings.Split(session.LoadedSkills, ",") {
			name = strings.TrimSpace(name)
			if name != "" {
				preloadedSkills = append(preloadedSkills, name)
			}
		}
	}

	agentCfg := agent.AgentConfig{
		System:          systemPrompt,
		Messages:        providerMessages,
		MCPServers:      capMCPServers,
		SkillTools:      skillTools,
		PreloadedSkills: preloadedSkills,
		ExtraTools:      coreTools,
		LiveUsage:       &liveUsage,
		SandboxDir:      sandboxDir,
		UserDir:         userID,
		Executor:        s.executor,
		SecretEnv:       secretEnv,
	}

	// Thread cancellation context
	if opts != nil && opts.Ctx != nil {
		agentCfg.Ctx = opts.Ctx
	}

	// Configure agent callbacks
	if opts != nil && opts.OnStreamChunk != nil {
		agentCfg.OnStreamChunk = opts.OnStreamChunk
	} else {
		agentCfg.OnStreamChunk = func(_ string, delta string) {
			emit("chunk", map[string]string{"delta": delta})
		}
	}

	// Thinking/reasoning chunks (always emit via SSE)
	agentCfg.OnThinkingChunk = func(_ string, delta string) {
		emit("thinking", map[string]string{"delta": delta})
	}

	if opts != nil && opts.OnToolCall != nil {
		agentCfg.OnToolCall = opts.OnToolCall
	} else {
		agentCfg.OnToolCall = func(toolName, input, output string, durationMs int64) {
			emit("tool_call", map[string]any{
				"tool":        toolName,
				"input":       input,
				"output":      output,
				"duration_ms": durationMs,
			})
		}
	}

	if opts != nil && opts.OnContextCompact != nil {
		agentCfg.OnContextCompact = opts.OnContextCompact
	} else {
		agentCfg.OnContextCompact = func(summary string, originalTokens, compactedTokens int) {
			session.Summary = summary
			emit("context_compact", map[string]any{
				"summary":          summary,
				"original_tokens":  originalTokens,
				"compacted_tokens": compactedTokens,
			})
		}
	}

	if opts != nil && opts.OnStepStart != nil {
		agentCfg.OnStepStart = opts.OnStepStart
	}
	if opts != nil && opts.OnStepDone != nil {
		agentCfg.OnStepDone = opts.OnStepDone
	}

	// AskUser: agent can ask the user questions in Chat mode.
	// Sends an SSE event, holds the agent loop, waits for continue/abort.
	agentCfg.AskUserFn = func(question string, options []string) (string, error) {
		// 1. Emit ask_user event to frontend
		emit("ask_user", map[string]interface{}{
			"question": question,
			"options":  options,
		})

		// 2. Update session status
		session.Status = "waiting_for_user"
		s.chatStore.Save(userID, scope, session)

		// 3. Block until user responds via /continue or /abort
		holdCh := s.createHoldChannel(sessionID)
		timeout := time.After(5 * time.Minute)

		select {
		case signal := <-holdCh:
			session.Status = "running"
			s.chatStore.Save(userID, scope, session)

			if signal.Action == "abort" {
				return "", fmt.Errorf("user declined to answer")
			}
			// The user's answer comes through signal.Action as "continue"
			// or a custom response via the body of the continue request
			if signal.Answer != "" {
				return signal.Answer, nil
			}
			return "User confirmed", nil

		case <-timeout:
			s.removeHoldChannel(sessionID)
			session.Status = "running"
			s.chatStore.Save(userID, scope, session)
			return "", fmt.Errorf("user did not respond within 5 minutes")
		}
	}

	p, err := provider.NewForModel(resolvedModel, apiKey, provider.WithDefaultRetry())
	if err != nil {
		return nil, fmt.Errorf("failed to create provider: %w", err)
	}
	agentCfg.Provider = p
	agentCfg.Model = resolvedModel

	// 9. Run agent loop
	ctx := models.NewExecutionContext("chat", userID, s.config.HomeDir)
	ctx.DB = s.db
	defer ctx.Close()

	log.Printf("💬 [chat:%s] user=%s model=%s skills=%v scope=%s",
		sessionID[:8], userID, resolvedModel, skillNames, scope)

	agentResult, err := agent.RunAgentLoop(agentCfg, ctx)
	if err != nil {
		session.Status = ""
		s.chatStore.Save(userID, scope, session)
		return nil, fmt.Errorf("agent error: %w", err)
	}

	// 10. Persist assistant/tool messages to session XML.
	// User message was already saved at step 7 before the agent loop ran.
	for _, msg := range agentResult.Messages {
		chatMsg := chat.Message{
			Role:    msg.Role,
			Content: msg.Content,
		}
		if msg.Role == "tool" {
			chatMsg.CallID = msg.ToolCallID
			chatMsg.Name = msg.ToolName
		}
		for _, tc := range msg.ToolCalls {
			chatMsg.ToolCalls = append(chatMsg.ToolCalls, chat.ToolCall{
				ID:    tc.ID,
				Name:  tc.Name,
				Input: tc.Arguments,
			})
		}
		session.AddMessage(chatMsg)
	}

	// Update usage
	session.Usage.InputTokens += agentResult.TotalUsage.InputTokens
	session.Usage.OutputTokens += agentResult.TotalUsage.OutputTokens
	session.Usage.Cost += agentResult.TotalCost

	// Auto-generate title from first user message
	firstTitle := session.Title == ""
	if firstTitle && len(session.Messages) > 0 {
		// Immediate: truncate first message as temporary title
		runes := []rune(message)
		if len(runes) > 50 {
			runes = runes[:50]
		}
		session.Title = string(runes)

		// Async: use AI to generate a better title (same model, no extra provider needed)
		go s.generateSessionTitle(userID, scope, sessionID, resolvedModel, apiKey, message)
	}

	// Update model if it was auto-resolved
	if session.Model == "" {
		session.Model = resolvedModel
	}

	// Persist loaded skills for next turn pre-activation
	if len(agentResult.LoadedSkills) > 0 {
		// Merge with existing loaded skills (dedup)
		existing := make(map[string]bool)
		if session.LoadedSkills != "" {
			for _, name := range strings.Split(session.LoadedSkills, ",") {
				name = strings.TrimSpace(name)
				if name != "" {
					existing[name] = true
				}
			}
		}
		for _, name := range agentResult.LoadedSkills {
			existing[name] = true
		}
		var allNames []string
		for name := range existing {
			allNames = append(allNames, name)
		}
		session.LoadedSkills = strings.Join(allNames, ",")
	}

	// Mark session as idle
	session.Status = ""
	session.HoldInfo = nil

	// Save session
	if err := s.chatStore.Save(userID, scope, session); err != nil {
		log.Printf("⚠️  [chat:%s] failed to save session: %v", sessionID[:8], err)
	}

	return agentResult, nil
}

// buildChatSystemPrompt creates the system prompt based on scope.
func (s *Server) buildChatSystemPrompt(userID, scope string) string {
	// Agent scope: load from workspace
	if agentName, ok := strings.CutPrefix(scope, chat.ScopeAgentPrefix); ok {
		if agentDef, err := s.workspace.ReadAgent(userID, agentName); err == nil {
			prompt := agentDef.SystemPrompt()
			if prompt == "" {
				prompt = "You are " + agentName + ", an AI agent."
			}
			// Add operational instructions from AGENTS.md
			if agentDef.AgentsMD != "" {
				prompt += "\n\n## Operational Instructions\n" + agentDef.AgentsMD
			}
			// App runs: AI output IS the deliverable — runtime handles notification delivery
			if strings.HasPrefix(agentName, "app-") {
				prompt += `

## Output Rules
Your text output is your ONLY deliverable. The platform runtime captures your final output and automatically delivers it to the configured notification channels (Telegram, Slack, Email, etc.).
- Do NOT mention or reference any notification channel (Telegram, Slack, etc.) in your output.
- Do NOT say "sending to..." or "delivered to...". Just produce the content.
- Write your output as if it will be read directly by the user — clean, concise, ready to consume.`
			}
			prompt += "\n\nCurrent time: " + time.Now().Format("2006-01-02 15:04:05 MST (Monday)")
			return prompt
		}
	}

	// User main chat: minimal system prompt — all detailed instructions live in skills
	prompt := fmt.Sprintf(`You are Tofi, a capable AI assistant.
Respond in the same language as the user. Be concise and helpful.
Think before acting. If a tool fails, try a different approach. Always deliver real results, never give up.

## Communication Style
When a task requires multiple tool calls, narrate your progress with brief text BEFORE making each tool call. This keeps the user informed:
- "Let me search for..." / "Found X, now checking..."  / "Looks like... let me try..."
- Keep narration to 1 short sentence, then make the tool call in the SAME response.
- Do NOT stay silent during multi-step work — the user should see your thought process.

## Notifications & Connectors
Tofi has built-in connectors for Telegram, Slack, Discord, and Email. These are configured by the user in Settings → Connectors and work automatically:
- When an App finishes running, the result is automatically delivered to configured notification channels.
- You do NOT need to install any skill or plugin to send messages to Telegram/Slack/Discord.
- If the user asks to "send results to Telegram" or similar, explain that they need to configure the Telegram connector in Settings, and then any App run will automatically notify them.
- Do NOT try to search for or install third-party Telegram/Slack/notification skills — the functionality is built-in.

Current time: %s`, time.Now().Format("2006-01-02 15:04:05 MST (Monday)"))

	return prompt
}

// getSystemSkillNames returns the names of all system-scope skills from the database.
// These are always available in every chat session (as deferred/on-demand skills).
func (s *Server) getSystemSkillNames() []string {
	records, err := s.db.ListSystemSkills()
	if err != nil {
		log.Printf("[chat] failed to list system skills: %v", err)
		return nil
	}
	var names []string
	for _, r := range records {
		names = append(names, r.Name)
	}
	return names
}

// appendSystemSkills adds system skill names to the list, avoiding duplicates.
func appendSystemSkills(skillNames []string, db *storage.DB) []string {
	sysNames, err := db.ListSystemSkillNames()
	if err != nil {
		return skillNames
	}

	existing := make(map[string]bool, len(skillNames))
	for _, n := range skillNames {
		existing[strings.TrimSpace(n)] = true
	}

	for _, name := range sysNames {
		if !existing[name] {
			skillNames = append(skillNames, name)
		}
	}
	return skillNames
}

// --- POST /api/v1/chat/sessions/{id}/continue ---

func (s *Server) handleChatSessionContinue(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(UserContextKey).(string)
	sessionID := r.PathValue("id")

	// Ownership check: a session ID alone must not let another user resume
	// or inject an answer into someone else's paused agent.
	idx, err := s.chatStore.GetIndex(sessionID)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, ErrSessionNotFound, "session not found", "")
		return
	}
	if idx.UserID != userID {
		writeJSONError(w, http.StatusForbidden, ErrForbidden, "forbidden", "")
		return
	}

	// Parse optional answer from request body
	var body struct {
		Answer string `json:"answer"`
	}
	json.NewDecoder(r.Body).Decode(&body) // ignore errors — answer is optional

	if !s.signalHold(sessionID, "continue", body.Answer) {
		writeJSONError(w, http.StatusConflict, ErrConflict, "no hold channel found for session", "")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "continued"})
}

// --- POST /api/v1/chat/sessions/{id}/abort ---

func (s *Server) handleChatSessionAbort(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(UserContextKey).(string)
	sessionID := r.PathValue("id")

	// Ownership check: without this anyone who guesses a session id can kill
	// another user's in-flight agent.
	idx, err := s.chatStore.GetIndex(sessionID)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, ErrSessionNotFound, "session not found", "")
		return
	}
	if idx.UserID != userID {
		writeJSONError(w, http.StatusForbidden, ErrForbidden, "forbidden", "")
		return
	}

	// Abort works at two layers:
	// 1. signalHold wakes an agent blocked on hold (skill install, ask_user)
	//    and tells it to skip/decline gracefully.
	// 2. cancelSession cancels the agent's context so mid-stream generation
	//    also stops.
	// At least one must succeed for the abort to report as applied.
	signalled := s.signalHold(sessionID, "abort", "")
	cancelled := s.cancelSession(sessionID)

	if !signalled && !cancelled {
		writeJSONError(w, http.StatusConflict, ErrConflict, "no active agent for session", "")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "aborted"})
}

// --- GET /api/v1/chat/sessions/{id}/stream ---
//
// Subscribe to an in-flight agent's SSE stream. Used after a browser refresh
// to resume following the same agent — e.g. after approving a skill install
// when the original POST /messages connection has already been torn down.
//
// If no agent is currently running for this session the endpoint emits a
// single "idle" event and closes, letting the client fall back to session
// detail polling.
//
// Honours the `Last-Event-ID` header so clients that reconnect mid-stream
// replay only events they haven't seen yet.
func (s *Server) handleChatSessionStream(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(UserContextKey).(string)
	sessionID := r.PathValue("id")

	idx, err := s.chatStore.GetIndex(sessionID)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, ErrSessionNotFound, "session not found", "")
		return
	}
	if idx.UserID != userID {
		writeJSONError(w, http.StatusForbidden, ErrForbidden, "forbidden", "")
		return
	}

	// Parse Last-Event-ID for replay offset (SSE reconnection convention).
	fromSeq := 0
	if v := r.Header.Get("Last-Event-ID"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			fromSeq = n
		}
	}

	hub := s.getSessionHub(sessionID)
	if hub == nil {
		// Agent already finished — emit an idle marker and close.
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		flusher, ok := w.(http.Flusher)
		if !ok {
			writeJSONError(w, http.StatusInternalServerError, ErrInternal, "streaming not supported", "")
			return
		}
		fmt.Fprintf(w, "event: idle\ndata: {}\n\n")
		flusher.Flush()
		return
	}

	streamSessionEventsToSSE(w, r, hub, sessionID, fromSeq)
}

// sendSSEEvent is a helper to send a named SSE event with JSON data.
func sendSSEEvent(w http.ResponseWriter, flusher http.Flusher, event string, data any) {
	b, _ := json.Marshal(data)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
	flusher.Flush()
}

// sendSSEError sends an error event over SSE.
func sendSSEError(w http.ResponseWriter, flusher http.Flusher, errMsg string) {
	sendSSEEvent(w, flusher, "error", map[string]string{"error": errMsg})
}

// buildChatWishTools exposes the marketplace search tool to user-scope chat.
// The agent cannot install skills itself — discovered skills are surfaced so
// the user can install them through the dashboard. A previous in-agent
// install path (tofi_suggest_install) was removed in v0.8.1: it had no real
// clone/fs step and lied to the agent about success.
func (s *Server) buildChatWishTools() []agent.ExtraBuiltinTool {
	return []agent.ExtraBuiltinTool{
		{
			Schema: provider.Tool{
				Name: "tofi_search",
				Description: "Search the skills.sh marketplace for skills the user could install. " +
					"Returns a listing only — you cannot install skills yourself. " +
					"If the user already has a skill covering the capability (shown in your system prompt), prefer tofi_load_skill over searching.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"query": map[string]interface{}{
							"type":        "string",
							"description": "Search query (e.g., 'react testing', 'code review', 'summarize')",
						},
					},
					"required": []string{"query"},
				},
			},
			Handler: func(args map[string]interface{}) (string, error) {
				query, _ := args["query"].(string)
				if query == "" {
					return "Error: query is required", nil
				}
				client := skills.NewRegistryClient("")
				result, err := client.Search(query, 5)
				if err != nil {
					return fmt.Sprintf("Search failed: %v", err), nil
				}
				if len(result.Skills) == 0 {
					return "No skills found for query: " + query, nil
				}
				var sb strings.Builder
				sb.WriteString(fmt.Sprintf("Found %d skills. You cannot install these directly — if one looks useful, tell the user the name + ID and suggest they install it from the dashboard.\n\n", len(result.Skills)))
				for _, sk := range result.Skills {
					sb.WriteString(fmt.Sprintf("- **%s** (%s): %s\n", sk.Name, sk.Source, sk.Description))
					sb.WriteString(fmt.Sprintf("  ID: %s\n", sk.ID))
				}
				return sb.String(), nil
			},
		},
	}
}

// buildSessionInfoTool creates a tool that lets the agent query current session info.
// liveUsage provides real-time token counts from the current agent loop iteration,
// so the total includes both historical usage and the in-progress loop.
func buildSessionInfoTool(session *chat.Session, model string, liveUsage *provider.Usage) agent.ExtraBuiltinTool {
	return agent.ExtraBuiltinTool{
		Schema: provider.Tool{
			Name:        "tofi_session_info",
			Description: "Get token usage, cost, model name, and message count for the current chat. ALWAYS use this tool when the user asks about usage, tokens, cost, billing, session info, or statistics. Returns: session ID, model, message count, input/output tokens, total cost, active skills.",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		Handler: func(args map[string]any) (string, error) {
			msgCount := len(session.Messages)
			// Combine historical session usage with in-progress agent loop usage
			inTok := session.Usage.InputTokens
			outTok := session.Usage.OutputTokens
			cost := session.Usage.Cost
			if liveUsage != nil {
				inTok += liveUsage.InputTokens
				outTok += liveUsage.OutputTokens
				cost += provider.CalculateCost(model, *liveUsage)
			}
			info := fmt.Sprintf(
				"Session: %s\nModel: %s\nMessages: %d\nInput tokens: %d\nOutput tokens: %d\nTotal cost: $%.4f",
				session.ID, model, msgCount, inTok, outTok, cost,
			)
			if session.Skills != "" {
				info += "\nSkills: " + session.Skills
			}
			if session.Title != "" {
				info += "\nTitle: " + session.Title
			}
			return info, nil
		},
	}
}

// generateSessionTitle uses AI to create a concise session title from the first user message.
// Runs asynchronously — updates the session title in storage when done.
func (s *Server) generateSessionTitle(userID, scope, sessionID, model, apiKey, firstMessage string) {
	p, err := provider.NewForModel(model, apiKey, provider.WithDefaultRetry())
	if err != nil {
		log.Printf("⚠️  [title:%s] failed to create provider: %v", sessionID[:8], err)
		return
	}

	// Truncate long messages to save tokens
	msgRunes := []rune(firstMessage)
	if len(msgRunes) > 200 {
		msgRunes = msgRunes[:200]
	}

	req := &provider.ChatRequest{
		Model:  model,
		System: "Generate a short title (max 20 characters) for a chat session based on the user's first message. Reply with ONLY the title, no quotes, no punctuation, no explanation. Use the SAME language as the user's message.",
		Messages: []provider.Message{
			{Role: "user", Content: string(msgRunes)},
		},
	}

	resp, err := p.Chat(context.Background(), req)
	if err != nil {
		log.Printf("⚠️  [title:%s] AI title generation failed: %v", sessionID[:8], err)
		return
	}

	title := strings.TrimSpace(resp.Content)
	if title == "" {
		return
	}

	// Enforce max length
	titleRunes := []rune(title)
	if len(titleRunes) > 30 {
		titleRunes = titleRunes[:30]
	}
	title = string(titleRunes)

	// Load → update title → save
	session, err := s.chatStore.Load(userID, scope, sessionID)
	if err != nil {
		log.Printf("⚠️  [title:%s] failed to load session: %v", sessionID[:8], err)
		return
	}
	session.Title = title
	if err := s.chatStore.Save(userID, scope, session); err != nil {
		log.Printf("⚠️  [title:%s] failed to save title: %v", sessionID[:8], err)
		return
	}
	log.Printf("✅ [title:%s] %s", sessionID[:8], title)
}

// --- GET /api/v1/chat/sessions/{id}/export ---

// handleExportSession exports a session as JSON or Markdown.
// Query params: format=json (default) | markdown
func (s *Server) handleExportSession(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(UserContextKey).(string)
	sessionID := r.PathValue("id")

	idx, err := s.chatStore.GetIndex(sessionID)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, ErrSessionNotFound, "session not found", "")
		return
	}
	if idx.UserID != userID {
		writeJSONError(w, http.StatusForbidden, ErrForbidden, "forbidden", "")
		return
	}

	session, err := s.chatStore.LoadByID(sessionID)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, ErrSessionNotFound, "session not found: "+err.Error(), "")
		return
	}

	format := r.URL.Query().Get("format")
	if format == "" {
		format = "json"
	}

	switch format {
	case "markdown":
		md := formatSessionMarkdown(session)
		filename := fmt.Sprintf("session-%s.md", sessionID)
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
		w.Write([]byte(md))

	case "json":
		filename := fmt.Sprintf("session-%s.json", sessionID)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
		json.NewEncoder(w).Encode(session)

	default:
		writeJSONError(w, http.StatusBadRequest, ErrBadRequest, "invalid format: use 'json' or 'markdown'", "")
	}
}

func formatSessionMarkdown(session *chat.Session) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# %s\n\n", session.Title))
	sb.WriteString(fmt.Sprintf("- **Session ID:** %s\n", session.ID))
	sb.WriteString(fmt.Sprintf("- **Model:** %s\n", session.Model))
	sb.WriteString(fmt.Sprintf("- **Created:** %s\n", session.Created))
	if session.Usage.InputTokens > 0 || session.Usage.OutputTokens > 0 {
		sb.WriteString(fmt.Sprintf("- **Tokens:** %d in / %d out ($%.4f)\n",
			session.Usage.InputTokens, session.Usage.OutputTokens, session.Usage.Cost))
	}
	sb.WriteString("\n---\n\n")

	for _, msg := range session.Messages {
		if msg.Content == "" && len(msg.ToolCalls) == 0 {
			continue
		}
		role := msg.Role
		switch role {
		case "user":
			role = "User"
		case "assistant":
			role = "Assistant"
		case "system":
			role = "System"
		case "tool":
			role = fmt.Sprintf("Tool (%s)", msg.Name)
		}

		if msg.Timestamp != "" {
			sb.WriteString(fmt.Sprintf("### %s — %s\n\n", role, msg.Timestamp))
		} else {
			sb.WriteString(fmt.Sprintf("### %s\n\n", role))
		}

		if msg.Content != "" {
			sb.WriteString(msg.Content)
			sb.WriteString("\n\n")
		}

		for _, tc := range msg.ToolCalls {
			sb.WriteString(fmt.Sprintf("> **Tool call:** `%s`\n>\n> ```json\n> %s\n> ```\n\n", tc.Name, tc.Input))
		}
	}

	return sb.String()
}

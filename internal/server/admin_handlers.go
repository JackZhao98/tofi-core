package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"tofi-core/internal/storage"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// --- Admin Request/Response Structs ---

type CreateUserRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"` // admin or user
}

type UserResponse struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	Role      string `json:"role"`
	CreatedAt string `json:"created_at"`
}



// --- Admin Handlers ---

// handleAdminListUsers 返回所有用户列表
func (s *Server) handleAdminListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.db.ListAllUsers()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, ErrInternal, err.Error(), "")
		return
	}

	resp := []UserResponse{}
	for _, u := range users {
		resp = append(resp, UserResponse{
			ID:        u.ID,
			Username:  u.Username,
			Role:      u.Role,
			CreatedAt: u.CreatedAt.String,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleAdminCreateUser 创建新用户
func (s *Server) handleAdminCreateUser(w http.ResponseWriter, r *http.Request) {
	var req CreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, ErrBadRequest, "Invalid request body", "")
		return
	}

	if req.Username == "" || req.Password == "" {
		writeJSONError(w, http.StatusBadRequest, ErrBadRequest, "Username and password are required", "")
		return
	}

	// 默认角色为 user
	if req.Role != "admin" && req.Role != "user" {
		req.Role = "user"
	}

	// 检查用户名是否已存在
	existing, _ := s.db.GetUser(req.Username)
	if existing != nil {
		writeJSONError(w, http.StatusConflict, ErrConflict, "Username already exists", "")
		return
	}

	// 密码哈希
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, ErrInternal, "Failed to hash password", "")
		return
	}

	id := uuid.New().String()
	if err := s.db.SaveUser(id, req.Username, string(hash), req.Role); err != nil {
		writeJSONError(w, http.StatusInternalServerError, ErrInternal, err.Error(), "")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{
		"id":      id,
		"message": "User created successfully",
	})
}

// handleAdminDeleteUser 删除用户
func (s *Server) handleAdminDeleteUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, ErrBadRequest, "User ID is required", "")
		return
	}

	// Cannot delete self
	currentUser := r.Context().Value(UserContextKey).(string)
	targetUser, err := s.db.GetUserByID(id)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, ErrNotFound, "User not found", "")
		return
	}
	if targetUser.Username == currentUser {
		writeJSONError(w, http.StatusForbidden, ErrForbidden, "Cannot delete your own account", "")
		return
	}

	// Cannot delete last admin
	if targetUser.Role == "admin" {
		users, _ := s.db.ListAllUsers()
		adminCount := 0
		for _, u := range users {
			if u.Role == "admin" {
				adminCount++
			}
		}
		if adminCount <= 1 {
			writeJSONError(w, http.StatusForbidden, ErrForbidden, "Cannot delete the last admin account", "")
			return
		}
	}

	if err := s.db.DeleteUser(id); err != nil {
		writeJSONError(w, http.StatusInternalServerError, ErrInternal, "Failed to delete user", "")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleAdminGetStats 返回系统统计
func (s *Server) handleAdminGetStats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.db.GetSystemStats()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, ErrInternal, err.Error(), "")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// handleAdminGetCostSummary returns the global-spend dashboard header:
// today / this month / all-time real USD cost across all users, plus
// active-user and run counts.
// GET /api/v1/admin/cost/summary
func (s *Server) handleAdminGetCostSummary(w http.ResponseWriter, r *http.Request) {
	summary, err := s.db.GetGlobalSpendSummary()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, ErrInternal, err.Error(), "")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(summary)
}

// handleAdminListUserSpending returns every user ranked by all-time spend
// (desc) with today/month/all-time cost and today's run count. This is the
// main "who is costing me money" view.
// GET /api/v1/admin/cost/users
func (s *Server) handleAdminListUserSpending(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.ListUserSpending()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, ErrInternal, err.Error(), "")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(rows)
}

// handleAdminGetModelBreakdown returns global usage grouped by model for
// an optional date range. Used for the "which provider is eating the most
// budget" chart.
// GET /api/v1/admin/cost/by-model?since=YYYY-MM-DD&until=YYYY-MM-DD
func (s *Server) handleAdminGetModelBreakdown(w http.ResponseWriter, r *http.Request) {
	since := r.URL.Query().Get("since")
	until := r.URL.Query().Get("until")

	// Default window: start of this month through now + 1 day (inclusive).
	if since == "" {
		since = time.Now().UTC().Format("2006-01") + "-01"
	}
	if until == "" {
		until = time.Now().UTC().AddDate(0, 0, 1).Format("2006-01-02")
	}

	usage, err := s.db.GetUsageByModel("", since, until)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, ErrInternal, err.Error(), "")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(usage)
}

// handleAdminGetRevenue returns the current month's revenue-vs-cost P/L
// summary: active subscriptions × plan price = revenue, chat_sessions cost
// sum = cost, net = the difference. Answers "am I making money yet?"
// GET /api/v1/admin/cost/revenue
func (s *Server) handleAdminGetRevenue(w http.ResponseWriter, r *http.Request) {
	summary, err := s.db.GetMonthlyRevenue(PlanPrice)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, ErrInternal, err.Error(), "")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(summary)
}

// UserDetailResponse is the full profile the admin detail page shows:
// basic account info + subscription state + usage buckets + favorite model
// + recent subscription event history.
type UserDetailResponse struct {
	ID                   string                     `json:"id"`
	Username             string                     `json:"username"`
	Role                 string                     `json:"role"`
	CreatedAt            string                     `json:"created_at"`
	Plan                 string                     `json:"plan"`
	Subscription         *SubscriptionInfo          `json:"subscription,omitempty"`
	SpendToday           float64                    `json:"spend_today"`
	SpendMonth           float64                    `json:"spend_month"`
	SpendAllTime         float64                    `json:"spend_all_time"`
	RunsToday            int                        `json:"runs_today"`
	RunsBySource         map[string]int             `json:"runs_by_source"`
	FavoriteModel        *storage.FavoriteModel     `json:"favorite_model,omitempty"`
	LastActiveAt         string                     `json:"last_active_at"`
	Events               []*storage.SubscriptionEvent `json:"events"`
}

// SubscriptionInfo is the UI-friendly subset of storage.SubscriptionRecord
// — we hide stripe_subscription_id from the API response (it leaks Stripe
// implementation detail without adding admin value).
type SubscriptionInfo struct {
	Plan                 string `json:"plan"`
	Status               string `json:"status"`
	CurrentPeriodStart   string `json:"current_period_start"`
	CurrentPeriodEnd     string `json:"current_period_end"`
	CancelAtPeriodEnd    bool   `json:"cancel_at_period_end"`
	StripeCustomerID     string `json:"stripe_customer_id"`
}

// handleAdminGetUserDetails returns the full admin-view profile for one
// user — identified by username in the path (that's the key every other
// table uses; see the note in storage/admin_metrics.go).
// GET /api/v1/admin/users/{username}/details
func (s *Server) handleAdminGetUserDetails(w http.ResponseWriter, r *http.Request) {
	username := r.PathValue("username")
	if username == "" {
		writeJSONError(w, http.StatusBadRequest, ErrBadRequest, "username required", "")
		return
	}

	user, err := s.db.GetUser(username)
	if err != nil || user == nil {
		writeJSONError(w, http.StatusNotFound, ErrNotFound, "user not found", "")
		return
	}

	now := time.Now().UTC()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	epoch := time.Unix(0, 0)

	plan, _ := s.db.GetUserPlan(username)
	spendToday, _ := s.db.GetUserSpend(username, todayStart)
	spendMonth, _ := s.db.GetUserSpend(username, monthStart)
	spendAll, _ := s.db.GetUserSpend(username, epoch)
	runsToday, _ := s.db.CountDailyAgentRuns(username)
	runsBySource, _ := s.db.CountDailyAgentRunsBySource(username)
	favorite, _ := s.db.GetUserFavoriteModel(username)
	lastActive, _ := s.db.GetUserLastActive(username)
	events, _ := s.db.ListSubscriptionEvents(username, 50)

	var subInfo *SubscriptionInfo
	if sub, _ := s.db.GetSubscription(username); sub != nil {
		subInfo = &SubscriptionInfo{
			Plan:               sub.Plan,
			Status:             sub.Status,
			CurrentPeriodStart: sub.CurrentPeriodStart,
			CurrentPeriodEnd:   sub.CurrentPeriodEnd,
			CancelAtPeriodEnd:  sub.CancelAtPeriodEnd,
			StripeCustomerID:   sub.StripeCustomerID,
		}
	}

	resp := UserDetailResponse{
		ID:            user.ID,
		Username:      user.Username,
		Role:          user.Role,
		CreatedAt:     user.CreatedAt.String,
		Plan:          plan,
		Subscription:  subInfo,
		SpendToday:    spendToday,
		SpendMonth:    spendMonth,
		SpendAllTime:  spendAll,
		RunsToday:     runsToday,
		RunsBySource:  runsBySource,
		FavoriteModel: favorite,
		LastActiveAt:  lastActive,
		Events:        events,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleAdminSetUserPlan gives admin manual override over a user's plan
// and/or current_period_end. Every mutation logs a 'admin_override' row
// into subscription_events so the audit trail stays complete.
// PUT /api/v1/admin/users/{username}/plan
//
// Body: { plan?: "free"|"developer", current_period_end?: "YYYY-MM-DDTHH:MM:SSZ", cancel_at_period_end?: bool, status?: "active"|"canceled"|"past_due" }
// Any subset of fields may be present. Fields omitted from the body are
// left as-is.
func (s *Server) handleAdminSetUserPlan(w http.ResponseWriter, r *http.Request) {
	username := r.PathValue("username")
	if username == "" {
		writeJSONError(w, http.StatusBadRequest, ErrBadRequest, "username required", "")
		return
	}

	user, err := s.db.GetUser(username)
	if err != nil || user == nil {
		writeJSONError(w, http.StatusNotFound, ErrNotFound, "user not found", "")
		return
	}

	var req struct {
		Plan              *string `json:"plan"`
		CurrentPeriodEnd  *string `json:"current_period_end"`
		CancelAtPeriodEnd *bool   `json:"cancel_at_period_end"`
		Status            *string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, ErrBadRequest, "invalid body: "+err.Error(), "")
		return
	}

	// Validate plan if supplied.
	if req.Plan != nil {
		if _, ok := PlanDefs[*req.Plan]; !ok {
			writeJSONError(w, http.StatusBadRequest, ErrBadRequest, "unknown plan '"+*req.Plan+"'", "")
			return
		}
	}

	// Load-or-init the subscription row so partial updates merge cleanly.
	existing, _ := s.db.GetSubscription(username)
	if existing == nil {
		existing = &storage.SubscriptionRecord{
			UserID: username,
			Plan:   "free",
			Status: "active",
		}
	}

	fromPlan := existing.Plan
	if req.Plan != nil {
		existing.Plan = *req.Plan
	}
	if req.CurrentPeriodEnd != nil {
		existing.CurrentPeriodEnd = *req.CurrentPeriodEnd
	}
	if req.CancelAtPeriodEnd != nil {
		existing.CancelAtPeriodEnd = *req.CancelAtPeriodEnd
	}
	if req.Status != nil {
		existing.Status = *req.Status
	}

	if err := s.db.UpsertSubscription(existing); err != nil {
		writeJSONError(w, http.StatusInternalServerError, ErrInternal, "upsert failed: "+err.Error(), "")
		return
	}

	// Audit event — keeps the immutable history the detail page shows.
	admin := r.Context().Value(UserContextKey).(string)
	metaJSON, _ := json.Marshal(map[string]any{"admin": admin, "request": req})
	_ = s.db.LogSubscriptionEvent(username, "admin_override", fromPlan, existing.Plan, "", string(metaJSON))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
}

// handleAdminGetUsage returns usage statistics grouped by model
func (s *Server) handleAdminGetUsage(w http.ResponseWriter, r *http.Request) {
	month := r.URL.Query().Get("month")   // e.g., "2026-03"
	userID := r.URL.Query().Get("user_id") // optional

	var startDate, endDate string
	if month != "" {
		// Parse "YYYY-MM" into date range
		startDate = month + "-01"
		// Calculate next month
		parts := strings.SplitN(month, "-", 2)
		if len(parts) == 2 {
			year, _ := strconv.Atoi(parts[0])
			mon, _ := strconv.Atoi(parts[1])
			mon++
			if mon > 12 {
				mon = 1
				year++
			}
			endDate = fmt.Sprintf("%04d-%02d-01", year, mon)
		}
	}

	usage, err := s.db.GetUsageByModel(userID, startDate, endDate)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, ErrInternal, err.Error(), "")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(usage)
}

// --- Admin Secrets Handlers ---

type SecretInfo struct {
	ID        string `json:"id"`
	User      string `json:"user"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// handleAdminListSecrets 返回所有用户的 secrets（仅元数据，不含加密值）
func (s *Server) handleAdminListSecrets(w http.ResponseWriter, r *http.Request) {
	secrets, err := s.db.ListAllSecrets()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, ErrInternal, err.Error(), "")
		return
	}

	resp := []SecretInfo{}
	for _, sec := range secrets {
		resp = append(resp, SecretInfo{
			ID:        sec.ID,
			User:      sec.User,
			Name:      sec.Name,
			CreatedAt: sec.CreatedAt.String,
			UpdatedAt: sec.UpdatedAt.String,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleAdminDeleteSecret 删除指定 ID 的 secret
func (s *Server) handleAdminDeleteSecret(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, ErrBadRequest, "Secret ID is required", "")
		return
	}

	if err := s.db.DeleteSecretByID(id); err != nil {
		writeJSONError(w, http.StatusNotFound, ErrNotFound, "Secret not found", "")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

package server

// PlanLimits defines the feature/quota limits for a subscription plan.
type PlanLimits struct {
	MaxApps        int  `json:"max_apps"`        // 0 = unlimited
	DailyRuns      int  `json:"daily_runs"`
	ConcurrentRuns int  `json:"concurrent_runs"`
	WebhookAPI     bool `json:"webhook_api"`
	CustomCron     bool `json:"custom_cron"`
	EmailNotify    bool `json:"email_notify"`
	RunHistoryDays int  `json:"run_history_days"` // 0 = unlimited
}

// PlanDefs maps plan names to their limits. Only two plans for now.
var PlanDefs = map[string]PlanLimits{
	"free": {
		MaxApps:        3,
		DailyRuns:      20,
		ConcurrentRuns: 1,
		WebhookAPI:     true,
		CustomCron:     false,
		EmailNotify:    false,
		RunHistoryDays: 1,
	},
	"developer": {
		MaxApps:        0, // unlimited
		DailyRuns:      100,
		ConcurrentRuns: 3,
		WebhookAPI:     true,
		CustomCron:     true,
		EmailNotify:    true,
		RunHistoryDays: 0, // unlimited
	},
}

// PlanPrice maps plan names to their monthly price in USD cents.
var PlanPrice = map[string]int64{
	"free":      0,
	"developer": 500, // $5.00
}

// AdminLimits is used for admin users — no restrictions.
var AdminLimits = PlanLimits{
	MaxApps:        0,
	DailyRuns:      999999,
	ConcurrentRuns: 999,
	WebhookAPI:     true,
	CustomCron:     true,
	EmailNotify:    true,
	RunHistoryDays: 0,
}

// getUserPlanLimits returns the plan limits for a user. Admin users have no limits.
func (s *Server) getUserPlanLimits(userID string) PlanLimits {
	// Admin bypass — no limits
	if user, err := s.db.GetUser(userID); err == nil && user.Role == "admin" {
		return AdminLimits
	}
	plan, _ := s.db.GetUserPlan(userID)
	if limits, ok := PlanDefs[plan]; ok {
		return limits
	}
	return PlanDefs["free"]
}

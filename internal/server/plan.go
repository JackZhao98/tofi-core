package server

import "tofi-core/internal/models"

// PlanLimits defines the feature/quota limits for a subscription plan.
type PlanLimits struct {
	MaxApps        int  `json:"max_apps"`        // 0 = unlimited
	DailyRuns      int  `json:"daily_runs"`
	MonthlyRuns    int  `json:"monthly_runs"`    // 0 = unlimited — hard ceiling regardless of daily
	ConcurrentRuns int  `json:"concurrent_runs"`
	WebhookAPI     bool `json:"webhook_api"`
	CustomCron     bool `json:"custom_cron"`
	EmailNotify    bool `json:"email_notify"`
	RunHistoryDays int  `json:"run_history_days"` // 0 = unlimited

	// AllowedSkillTiers enumerates which skill tiers this plan can use.
	// A skill with no Tier (or Tier="free") is always allowed. A nil slice
	// means "admin bypass — everything is allowed".
	//
	// Example: developer plan = {"free", "developer"} — free and developer
	// skills both pass, pro skills are denied.
	AllowedSkillTiers []string `json:"allowed_skill_tiers"`
}

// PlanDefs maps plan names to their limits.
//
// Convention: any integer cap set to 0 means "unlimited" — the enforcement
// sites all guard on `if cap > 0 { check }`, so the zero path trivially
// short-circuits. Admin is the single tier with every cap at zero.
var PlanDefs = map[string]PlanLimits{
	"free": {
		MaxApps:           3,
		DailyRuns:         20,
		MonthlyRuns:       0, // not yet enforced for free — overage tier follow-up
		ConcurrentRuns:    1,
		WebhookAPI:        true,
		CustomCron:        false,
		EmailNotify:       false,
		RunHistoryDays:    1,
		AllowedSkillTiers: []string{"free"},
	},
	"developer": {
		MaxApps:           0, // unlimited
		DailyRuns:         100,
		MonthlyRuns:       0, // cap rollout handled in follow-up commit
		ConcurrentRuns:    3,
		WebhookAPI:        true,
		CustomCron:        true,
		EmailNotify:       true,
		RunHistoryDays:    0, // unlimited
		AllowedSkillTiers: []string{"free", "developer"},
	},
	// Pro unlocks higher-margin skills (pro_pack) + higher concurrency
	// for teams/power users. Numbers were sized against a ~$0.018
	// average cost/run: E[cost] ~ $18 leaves ~25-40% margin at the
	// $29.99 founding / $39.99 steady-state prices.
	"pro": {
		MaxApps:           0, // unlimited
		DailyRuns:         100,
		MonthlyRuns:       3000,
		ConcurrentRuns:    5,
		WebhookAPI:        true,
		CustomCron:        true,
		EmailNotify:       true,
		RunHistoryDays:    0, // unlimited
		AllowedSkillTiers: []string{"free", "developer", "pro"},
	},
	// Admin is a non-purchasable, invite-only tier. No limits of any kind.
	// AdminLimits mirrors this so legacy callers that reach for the
	// package-level var instead of PlanDefs["admin"] still get zeros.
	"admin": {
		MaxApps:           0,
		DailyRuns:         0,
		MonthlyRuns:       0,
		ConcurrentRuns:    0,
		WebhookAPI:        true,
		CustomCron:        true,
		EmailNotify:       true,
		RunHistoryDays:    0,
		AllowedSkillTiers: nil, // nil = bypass — admin sees every skill
	},
}

// PlanPrice maps plan names to their monthly price in USD cents. This is
// the number the admin revenue dashboard accrues against, so it must
// track the price users are *actually* charged — not the designed /
// aspirational / launch price. Update this in lock-step with the Stripe
// price ID migration, not before. Admin is not purchasable so it
// contributes $0 to MRR.
var PlanPrice = map[string]int64{
	"free":      0,
	"developer": 500, // $5.00 — current Stripe price (early adopter rate)
	"pro":       0,   // no Stripe price yet — migration pending
	"admin":     0,
}

// AdminLimits is used for admin users — no restrictions. Zero values
// mean "unlimited" by the convention above.
var AdminLimits = PlanDefs["admin"]

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

// PlanAllowsSkill reports whether a skill at the given tier is usable by
// the provided plan. Empty tier is normalized to "free" so untagged skills
// stay universally available. A nil AllowedSkillTiers means admin bypass.
func PlanAllowsSkill(limits PlanLimits, skillTier string) bool {
	if limits.AllowedSkillTiers == nil {
		return true // admin / bypass
	}
	if skillTier == "" {
		skillTier = "free"
	}
	for _, t := range limits.AllowedSkillTiers {
		if t == skillTier {
			return true
		}
	}
	return false
}

// FilterSkillsByPlan returns only the manifests the plan is entitled to
// use, preserving the input order. Use this upstream of the planner LLM
// so it never recommends skills the user can't call.
func FilterSkillsByPlan(catalog []models.SkillManifest, limits PlanLimits) []models.SkillManifest {
	out := make([]models.SkillManifest, 0, len(catalog))
	for _, m := range catalog {
		if PlanAllowsSkill(limits, m.Tier) {
			out = append(out, m)
		}
	}
	return out
}

package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// FavoriteModel describes the model a user has used the most (by session
// count). Session count is a better "where is this user spending their
// attention" signal than cost, because a user who chats a lot with a
// cheap model and occasionally uses an expensive one will have their
// attention correctly attributed to the cheap one.
type FavoriteModel struct {
	Model     string  `json:"model"`
	Sessions  int     `json:"sessions"`
	TotalCost float64 `json:"total_cost"`
}

// GetUserFavoriteModel returns the single model this user has the most
// sessions with, ever. Returns nil (not an error) if the user has no
// sessions yet — the admin UI renders a "—" in that case.
func (db *DB) GetUserFavoriteModel(userID string) (*FavoriteModel, error) {
	var fm FavoriteModel
	err := db.conn.QueryRow(`
		SELECT COALESCE(NULLIF(model, ''), '(unknown)') as model,
		       COUNT(*) as sessions,
		       COALESCE(SUM(total_cost), 0) as total_cost
		FROM chat_sessions
		WHERE user_id = ?
		GROUP BY model
		ORDER BY sessions DESC, total_cost DESC
		LIMIT 1
	`, userID).Scan(&fm.Model, &fm.Sessions, &fm.TotalCost)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get user favorite model: %w", err)
	}
	return &fm, nil
}

// RevenuePlanBucket is an active-subscription count + revenue contribution
// at the current month's price for a single plan tier.
type RevenuePlanBucket struct {
	Plan            string  `json:"plan"`
	ActiveCount     int     `json:"active_count"`
	PriceCents      int64   `json:"price_cents"`
	MonthlyRevenue  float64 `json:"monthly_revenue"` // USD
}

// RevenueSummary is the month's cash-in vs cash-out rollup used to answer
// "am I making money yet?" on the admin dashboard. Revenue is counted
// from active subscriptions at current plan prices; cost is the real
// provider spend for the month (chat_sessions.total_cost sum).
type RevenueSummary struct {
	Buckets       []RevenuePlanBucket `json:"buckets"`
	TotalRevenue  float64             `json:"total_revenue"`
	TotalCost     float64             `json:"total_cost"`
	NetMargin     float64             `json:"net_margin"`     // revenue - cost
	MarginPercent float64             `json:"margin_percent"` // (revenue - cost) / revenue * 100 (0 if revenue==0)
	Month         string              `json:"month"`          // YYYY-MM
}

// GetMonthlyRevenue returns the current month's revenue-vs-cost summary.
// Revenue uses PlanPrice-style info passed in via the pricing map (decoupled
// from the pricing config in server/plan.go to keep storage pure).
//
// pricing is a plan-name → price-in-cents map. Pass PlanPrice from the
// server package when calling this.
func (db *DB) GetMonthlyRevenue(pricing map[string]int64) (*RevenueSummary, error) {
	now := time.Now().UTC()
	monthStr := now.Format("2006-01")
	monthStart := monthStr + "-01"

	// Count active subs per plan — status='active' AND not past current_period_end.
	// SQLite stores timestamps as text so the comparison is lexicographic
	// on ISO-8601 strings, which is correct for our format.
	rows, err := db.conn.Query(`
		SELECT plan, COUNT(*) FROM user_subscriptions
		WHERE status = 'active'
		  AND (current_period_end = '' OR current_period_end >= ?)
		GROUP BY plan
	`, now.Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("query active subs: %w", err)
	}
	defer rows.Close()

	buckets := []RevenuePlanBucket{}
	totalRevenue := 0.0
	for rows.Next() {
		var plan string
		var cnt int
		if err := rows.Scan(&plan, &cnt); err != nil {
			return nil, err
		}
		price := pricing[plan]
		monthly := float64(price) * float64(cnt) / 100.0
		buckets = append(buckets, RevenuePlanBucket{
			Plan:           plan,
			ActiveCount:    cnt,
			PriceCents:     price,
			MonthlyRevenue: monthly,
		})
		totalRevenue += monthly
	}

	// Total provider cost for the month (summed across all chat_sessions).
	var totalCost float64
	_ = db.conn.QueryRow(`
		SELECT COALESCE(SUM(total_cost), 0) FROM chat_sessions WHERE created_at >= ?
	`, monthStart).Scan(&totalCost)

	net := totalRevenue - totalCost
	pct := 0.0
	if totalRevenue > 0 {
		pct = net / totalRevenue * 100
	}

	return &RevenueSummary{
		Buckets:       buckets,
		TotalRevenue:  totalRevenue,
		TotalCost:     totalCost,
		NetMargin:     net,
		MarginPercent: pct,
		Month:         monthStr,
	}, nil
}

// UserSpendRow is a ranked per-user spend record for the admin dashboard.
// Spend is real provider cost in USD (pre-markup) — never shown to end users,
// only to admin.
type UserSpendRow struct {
	UserID         string  `json:"user_id"`
	Username       string  `json:"username"`
	Plan           string  `json:"plan"`
	SpendToday     float64 `json:"spend_today"`
	SpendMonth     float64 `json:"spend_month"`
	SpendAllTime   float64 `json:"spend_all_time"`
	RunsToday      int     `json:"runs_today"`
	LastActiveAt   string  `json:"last_active_at"`
}

// ListUserSpending returns every user ranked by spend_all_time descending.
// Cheap enough to do in-process: for N <= a few hundred users this is a
// handful of small aggregate queries, all with existing indexes.
//
// The admin dashboard reads this whole list and filters/sorts client-side.
func (db *DB) ListUserSpending() ([]UserSpendRow, error) {
	users, err := db.ListAllUsers()
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}

	now := time.Now().UTC()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	epoch := time.Unix(0, 0)

	// Historical quirk: the users table keys by UUID (users.id), but every
	// downstream table — chat_sessions, user_subscriptions, agent_runs —
	// keys by the username/email string that the auth middleware sets as
	// the request userID. So to look up a user's rows we pass Username,
	// not ID. (Fixing the schema to be consistent is a separate migration.)
	out := make([]UserSpendRow, 0, len(users))
	for _, u := range users {
		plan, _ := db.GetUserPlan(u.Username)
		today, _ := db.GetUserSpend(u.Username, todayStart)
		month, _ := db.GetUserSpend(u.Username, monthStart)
		all, _ := db.GetUserSpend(u.Username, epoch)
		runsToday, _ := db.CountDailyAgentRuns(u.Username)
		lastActive, _ := db.GetUserLastActive(u.Username)

		out = append(out, UserSpendRow{
			UserID:       u.ID,
			Username:     u.Username,
			Plan:         plan,
			SpendToday:   today,
			SpendMonth:   month,
			SpendAllTime: all,
			RunsToday:    runsToday,
			LastActiveAt: lastActive,
		})
	}

	// Sort descending by SpendAllTime so the highest spenders are first.
	// Using an in-place insertion sort keeps the code dependency-free and
	// plenty fast for the expected user count.
	for i := 1; i < len(out); i++ {
		j := i
		for j > 0 && out[j-1].SpendAllTime < out[j].SpendAllTime {
			out[j-1], out[j] = out[j], out[j-1]
			j--
		}
	}
	return out, nil
}

// GetUserLastActive returns the timestamp of this user's most recent
// chat session or agent run, whichever is newer. Empty string if never.
func (db *DB) GetUserLastActive(userID string) (string, error) {
	var chatTs, runTs string
	_ = db.conn.QueryRow(`
		SELECT COALESCE(MAX(updated_at), '') FROM chat_sessions WHERE user_id = ?
	`, userID).Scan(&chatTs)
	_ = db.conn.QueryRow(`
		SELECT COALESCE(MAX(created_at), '') FROM run_events WHERE user_id = ?
	`, userID).Scan(&runTs)

	if runTs > chatTs {
		return runTs, nil
	}
	return chatTs, nil
}

// GlobalSpendSummary is the top-level "how much am I paying OpenAI etc.
// right now" aggregate for the admin dashboard header.
type GlobalSpendSummary struct {
	SpendToday      float64 `json:"spend_today"`
	SpendMonth      float64 `json:"spend_month"`
	SpendAllTime    float64 `json:"spend_all_time"`
	ActiveUsers     int     `json:"active_users"` // users with at least one run today
	TotalUsers      int     `json:"total_users"`
	RunsToday       int     `json:"runs_today"`
	RunsAllTime     int     `json:"runs_all_time"`
}

// GetGlobalSpendSummary returns system-wide spend across all users over the
// standard time buckets, plus active-user and run counts. No userID filter.
func (db *DB) GetGlobalSpendSummary() (*GlobalSpendSummary, error) {
	now := time.Now().UTC()
	todayStart := now.Format("2006-01-02")
	monthStart := now.Format("2006-01") + "-01"

	var today, month, all float64
	_ = db.conn.QueryRow(`
		SELECT COALESCE(SUM(total_cost), 0) FROM chat_sessions WHERE created_at >= ?
	`, todayStart).Scan(&today)
	_ = db.conn.QueryRow(`
		SELECT COALESCE(SUM(total_cost), 0) FROM chat_sessions WHERE created_at >= ?
	`, monthStart).Scan(&month)
	_ = db.conn.QueryRow(`
		SELECT COALESCE(SUM(total_cost), 0) FROM chat_sessions
	`).Scan(&all)

	var activeUsers, totalUsers, runsToday, runsAllTime int
	_ = db.conn.QueryRow(`
		SELECT COUNT(DISTINCT user_id) FROM run_events WHERE DATE(created_at) = ?
	`, todayStart).Scan(&activeUsers)
	_ = db.conn.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&totalUsers)
	_ = db.conn.QueryRow(`
		SELECT COUNT(*) FROM run_events WHERE DATE(created_at) = ?
	`, todayStart).Scan(&runsToday)
	_ = db.conn.QueryRow(`SELECT COUNT(*) FROM run_events`).Scan(&runsAllTime)

	return &GlobalSpendSummary{
		SpendToday:   today,
		SpendMonth:   month,
		SpendAllTime: all,
		ActiveUsers:  activeUsers,
		TotalUsers:   totalUsers,
		RunsToday:    runsToday,
		RunsAllTime:  runsAllTime,
	}, nil
}

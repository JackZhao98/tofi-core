package storage

import (
	"fmt"
	"time"
)

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
		SELECT COALESCE(MAX(created_at), '') FROM agent_runs WHERE user_id = ?
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
		SELECT COUNT(DISTINCT user_id) FROM agent_runs WHERE DATE(created_at) = ?
	`, todayStart).Scan(&activeUsers)
	_ = db.conn.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&totalUsers)
	_ = db.conn.QueryRow(`
		SELECT COUNT(*) FROM agent_runs WHERE DATE(created_at) = ?
	`, todayStart).Scan(&runsToday)
	_ = db.conn.QueryRow(`SELECT COUNT(*) FROM agent_runs`).Scan(&runsAllTime)

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

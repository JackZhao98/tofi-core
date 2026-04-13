package storage

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// SubscriptionRecord represents a user's current subscription state.
type SubscriptionRecord struct {
	UserID               string `json:"user_id"`
	Plan                 string `json:"plan"`
	Status               string `json:"status"`
	StripeCustomerID     string `json:"stripe_customer_id"`
	StripeSubscriptionID string `json:"stripe_subscription_id"`
	CurrentPeriodStart   string `json:"current_period_start"`
	CurrentPeriodEnd     string `json:"current_period_end"`
	CancelAtPeriodEnd    bool   `json:"cancel_at_period_end"`
	CreatedAt            string `json:"created_at"`
	UpdatedAt            string `json:"updated_at"`
}

// SubscriptionEvent is an immutable audit log entry.
type SubscriptionEvent struct {
	ID            string `json:"id"`
	UserID        string `json:"user_id"`
	EventType     string `json:"event_type"`
	FromPlan      string `json:"from_plan"`
	ToPlan        string `json:"to_plan"`
	StripeEventID string `json:"stripe_event_id"`
	Metadata      string `json:"metadata"`
	CreatedAt     string `json:"created_at"`
}

func (db *DB) initSubscriptionsTable() error {
	_, err := db.conn.Exec(`
		CREATE TABLE IF NOT EXISTS user_subscriptions (
			user_id TEXT PRIMARY KEY,
			plan TEXT NOT NULL DEFAULT 'free',
			status TEXT NOT NULL DEFAULT 'active',
			stripe_customer_id TEXT DEFAULT '',
			stripe_subscription_id TEXT DEFAULT '',
			current_period_start TEXT DEFAULT '',
			current_period_end TEXT DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS subscription_events (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			event_type TEXT NOT NULL,
			from_plan TEXT DEFAULT '',
			to_plan TEXT DEFAULT '',
			stripe_event_id TEXT DEFAULT '',
			metadata TEXT DEFAULT '{}',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_sub_events_user ON subscription_events(user_id);
	`)
	// Migration: add cancel_at_period_end column
	db.conn.Exec(`ALTER TABLE user_subscriptions ADD COLUMN cancel_at_period_end INTEGER DEFAULT 0`)
	return err
}

// GetUserPlan returns the plan for a user, defaulting to "free" if no row exists.
func (db *DB) GetUserPlan(userID string) (string, error) {
	var plan string
	err := db.conn.QueryRow(`SELECT plan FROM user_subscriptions WHERE user_id = ?`, userID).Scan(&plan)
	if err == sql.ErrNoRows {
		return "free", nil
	}
	if err != nil {
		return "free", fmt.Errorf("get user plan: %w", err)
	}
	return plan, nil
}

// GetSubscription returns the full subscription record. Returns nil if no row.
func (db *DB) GetSubscription(userID string) (*SubscriptionRecord, error) {
	var s SubscriptionRecord
	err := db.conn.QueryRow(`
		SELECT user_id, plan, status, stripe_customer_id, stripe_subscription_id,
		       current_period_start, current_period_end, cancel_at_period_end, created_at, updated_at
		FROM user_subscriptions WHERE user_id = ?
	`, userID).Scan(
		&s.UserID, &s.Plan, &s.Status, &s.StripeCustomerID, &s.StripeSubscriptionID,
		&s.CurrentPeriodStart, &s.CurrentPeriodEnd, &s.CancelAtPeriodEnd, &s.CreatedAt, &s.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get subscription: %w", err)
	}
	return &s, nil
}

// UpsertSubscription creates or updates the subscription row.
func (db *DB) UpsertSubscription(s *SubscriptionRecord) error {
	cancelInt := 0
	if s.CancelAtPeriodEnd {
		cancelInt = 1
	}
	_, err := db.conn.Exec(`
		INSERT INTO user_subscriptions (user_id, plan, status, stripe_customer_id, stripe_subscription_id,
		                                current_period_start, current_period_end, cancel_at_period_end, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET
			plan = excluded.plan,
			status = excluded.status,
			stripe_customer_id = excluded.stripe_customer_id,
			stripe_subscription_id = excluded.stripe_subscription_id,
			current_period_start = excluded.current_period_start,
			current_period_end = excluded.current_period_end,
			cancel_at_period_end = excluded.cancel_at_period_end,
			updated_at = excluded.updated_at
	`,
		s.UserID, s.Plan, s.Status, s.StripeCustomerID, s.StripeSubscriptionID,
		s.CurrentPeriodStart, s.CurrentPeriodEnd, cancelInt, time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("upsert subscription: %w", err)
	}
	return nil
}

// LogSubscriptionEvent inserts an immutable audit event.
func (db *DB) LogSubscriptionEvent(userID, eventType, fromPlan, toPlan, stripeEventID, metadata string) error {
	id := uuid.New().String()
	if metadata == "" {
		metadata = "{}"
	}
	_, err := db.conn.Exec(`
		INSERT INTO subscription_events (id, user_id, event_type, from_plan, to_plan, stripe_event_id, metadata)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, id, userID, eventType, fromPlan, toPlan, stripeEventID, metadata)
	if err != nil {
		return fmt.Errorf("log subscription event: %w", err)
	}
	return nil
}

// ListSubscriptionEvents returns events for a user, newest first.
func (db *DB) ListSubscriptionEvents(userID string, limit int) ([]*SubscriptionEvent, error) {
	rows, err := db.conn.Query(`
		SELECT id, user_id, event_type, from_plan, to_plan, stripe_event_id, metadata, created_at
		FROM subscription_events WHERE user_id = ? ORDER BY created_at DESC LIMIT ?
	`, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("list subscription events: %w", err)
	}
	defer rows.Close()

	var events []*SubscriptionEvent
	for rows.Next() {
		var e SubscriptionEvent
		if err := rows.Scan(&e.ID, &e.UserID, &e.EventType, &e.FromPlan, &e.ToPlan, &e.StripeEventID, &e.Metadata, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan subscription event: %w", err)
		}
		events = append(events, &e)
	}
	return events, nil
}

// CountUserApps returns the number of apps owned by a user.
func (db *DB) CountUserApps(userID string) (int, error) {
	var count int
	err := db.conn.QueryRow(`SELECT COUNT(*) FROM apps WHERE user_id = ?`, userID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count user apps: %w", err)
	}
	return count, nil
}

// CountDailyRuns returns runs dispatched today (UTC) for a user.
func (db *DB) CountDailyRuns(userID string) (int, error) {
	today := time.Now().UTC().Format("2006-01-02")
	var count int
	err := db.conn.QueryRow(`
		SELECT COUNT(*) FROM app_runs
		WHERE user_id = ? AND DATE(created_at) = ?
	`, userID, today).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count daily runs: %w", err)
	}
	return count, nil
}

// CountRunningRuns returns currently running runs for a user.
func (db *DB) CountRunningRuns(userID string) (int, error) {
	var count int
	err := db.conn.QueryRow(`
		SELECT COUNT(*) FROM app_runs
		WHERE user_id = ? AND status IN ('pending', 'running')
	`, userID, ).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count running runs: %w", err)
	}
	return count, nil
}

// GetSubscriptionByStripeCustomer finds a subscription by Stripe customer ID.
func (db *DB) GetSubscriptionByStripeCustomer(customerID string) (*SubscriptionRecord, error) {
	var s SubscriptionRecord
	err := db.conn.QueryRow(`
		SELECT user_id, plan, status, stripe_customer_id, stripe_subscription_id,
		       current_period_start, current_period_end, cancel_at_period_end, created_at, updated_at
		FROM user_subscriptions WHERE stripe_customer_id = ?
	`, customerID).Scan(
		&s.UserID, &s.Plan, &s.Status, &s.StripeCustomerID, &s.StripeSubscriptionID,
		&s.CurrentPeriodStart, &s.CurrentPeriodEnd, &s.CancelAtPeriodEnd, &s.CreatedAt, &s.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get subscription by stripe customer: %w", err)
	}
	return &s, nil
}

// GetExpiredSubscriptions returns paid subscriptions past their period end.
func (db *DB) GetExpiredSubscriptions() ([]*SubscriptionRecord, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	rows, err := db.conn.Query(`
		SELECT user_id, plan, status, stripe_customer_id, stripe_subscription_id,
		       current_period_start, current_period_end, cancel_at_period_end, created_at, updated_at
		FROM user_subscriptions
		WHERE plan != 'free' AND status = 'active' AND current_period_end != '' AND current_period_end < ?
	`, now)
	if err != nil {
		return nil, fmt.Errorf("get expired subscriptions: %w", err)
	}
	defer rows.Close()

	var subs []*SubscriptionRecord
	for rows.Next() {
		var s SubscriptionRecord
		if err := rows.Scan(&s.UserID, &s.Plan, &s.Status, &s.StripeCustomerID, &s.StripeSubscriptionID,
			&s.CurrentPeriodStart, &s.CurrentPeriodEnd, &s.CancelAtPeriodEnd, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan expired subscription: %w", err)
		}
		subs = append(subs, &s)
	}
	return subs, nil
}

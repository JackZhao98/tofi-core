package storage

import (
	"testing"
)

// TestAdminMetrics_EmptyDB: fresh DB with no users should give sane zero
// values, not errors. The admin dashboard should render even when the
// system is brand new.
func TestAdminMetrics_EmptyDB(t *testing.T) {
	db, err := InitDB(t.TempDir())
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer db.Close()

	summary, err := db.GetGlobalSpendSummary()
	if err != nil {
		t.Fatalf("GetGlobalSpendSummary: %v", err)
	}
	if summary.SpendAllTime != 0 || summary.RunsAllTime != 0 || summary.TotalUsers != 0 {
		t.Errorf("fresh DB should be all zeros, got %+v", summary)
	}

	rows, err := db.ListUserSpending()
	if err != nil {
		t.Fatalf("ListUserSpending: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("fresh DB should have no user rows, got %d", len(rows))
	}
}

// TestAdminMetrics_WithUsersAndRuns: create users + record some runs,
// verify the summary and per-user rankings reflect reality.
func TestAdminMetrics_WithUsersAndRuns(t *testing.T) {
	db, err := InitDB(t.TempDir())
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer db.Close()

	// Create two users
	if err := db.SaveUser("u1", "alice", "hash", "user"); err != nil {
		t.Fatalf("SaveUser alice: %v", err)
	}
	if err := db.SaveUser("u2", "bob", "hash", "user"); err != nil {
		t.Fatalf("SaveUser bob: %v", err)
	}

	// Alice gets 3 runs, Bob gets 1. Record by username — that's the
	// convention used by chat_sessions / agent_runs / user_subscriptions
	// (see the note in storage/admin_metrics.go about the schema quirk).
	for i := 0; i < 3; i++ {
		if err := db.RecordAgentRun("alice", "chat"); err != nil {
			t.Fatalf("RecordAgentRun for alice: %v", err)
		}
	}
	if err := db.RecordAgentRun("bob", "app"); err != nil {
		t.Fatalf("RecordAgentRun for bob: %v", err)
	}

	summary, err := db.GetGlobalSpendSummary()
	if err != nil {
		t.Fatalf("GetGlobalSpendSummary: %v", err)
	}
	if summary.TotalUsers != 2 {
		t.Errorf("total_users: want 2, got %d", summary.TotalUsers)
	}
	if summary.RunsToday != 4 {
		t.Errorf("runs_today: want 4, got %d", summary.RunsToday)
	}
	if summary.RunsAllTime != 4 {
		t.Errorf("runs_all_time: want 4, got %d", summary.RunsAllTime)
	}
	if summary.ActiveUsers != 2 {
		t.Errorf("active_users: want 2, got %d", summary.ActiveUsers)
	}

	rows, err := db.ListUserSpending()
	if err != nil {
		t.Fatalf("ListUserSpending: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 user rows, got %d", len(rows))
	}
	// Both have zero spend (no chat_sessions with cost), but runs_today
	// differs. Sort tie-break isn't specified, but we should at least find
	// alice with 3 runs and bob with 1.
	var aliceRuns, bobRuns int
	for _, r := range rows {
		switch r.Username {
		case "alice":
			aliceRuns = r.RunsToday
		case "bob":
			bobRuns = r.RunsToday
		}
	}
	if aliceRuns != 3 {
		t.Errorf("alice runs_today: want 3, got %d", aliceRuns)
	}
	if bobRuns != 1 {
		t.Errorf("bob runs_today: want 1, got %d", bobRuns)
	}
}

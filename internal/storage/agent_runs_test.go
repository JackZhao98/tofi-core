package storage

import (
	"testing"
)

func TestAgentRuns_RecordAndCount(t *testing.T) {
	db, err := InitDB(t.TempDir())
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer db.Close()

	const userA = "u_a"
	const userB = "u_b"

	// Empty — zero count
	if n, _ := db.CountDailyAgentRuns(userA); n != 0 {
		t.Errorf("empty state: want 0, got %d", n)
	}

	// Record three runs for user A across different sources, one for B
	for _, src := range []string{"chat", "chat", "app"} {
		if err := db.RecordAgentRun(userA, src); err != nil {
			t.Fatalf("RecordAgentRun(%q): %v", src, err)
		}
	}
	if err := db.RecordAgentRun(userB, "webhook"); err != nil {
		t.Fatalf("RecordAgentRun for userB: %v", err)
	}

	// User-scoped totals
	if n, _ := db.CountDailyAgentRuns(userA); n != 3 {
		t.Errorf("userA total: want 3, got %d", n)
	}
	if n, _ := db.CountDailyAgentRuns(userB); n != 1 {
		t.Errorf("userB total: want 1, got %d", n)
	}

	// Per-source breakdown for userA
	bySource, err := db.CountDailyAgentRunsBySource(userA)
	if err != nil {
		t.Fatalf("CountDailyAgentRunsBySource: %v", err)
	}
	if bySource["chat"] != 2 {
		t.Errorf("by-source chat: want 2, got %d", bySource["chat"])
	}
	if bySource["app"] != 1 {
		t.Errorf("by-source app: want 1, got %d", bySource["app"])
	}
	if bySource["webhook"] != 0 {
		t.Errorf("by-source webhook (userA had none): want 0, got %d", bySource["webhook"])
	}
}

func TestAgentRuns_EmptyUserID(t *testing.T) {
	db, err := InitDB(t.TempDir())
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer db.Close()

	// RecordAgentRun with empty userID should refuse — we don't want bad
	// data polluting the ledger
	if err := db.RecordAgentRun("", "chat"); err == nil {
		t.Error("expected error on empty userID, got nil")
	}
}

func TestAgentRuns_CountMonthly(t *testing.T) {
	db, err := InitDB(t.TempDir())
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer db.Close()

	const u = "u_monthly"

	if n, _ := db.CountMonthlyAgentRuns(u); n != 0 {
		t.Errorf("empty state: want 0, got %d", n)
	}

	// 5 runs in the current month — daily + monthly counts should match
	for i := 0; i < 5; i++ {
		if err := db.RecordAgentRun(u, "chat"); err != nil {
			t.Fatalf("RecordAgentRun: %v", err)
		}
	}

	if n, _ := db.CountMonthlyAgentRuns(u); n != 5 {
		t.Errorf("after 5 runs: want 5, got %d", n)
	}

	// A run from a previous month must not count — backfill a row directly
	// with an old created_at. We can't go through RecordAgentRun since it
	// uses CURRENT_TIMESTAMP.
	if _, err := db.conn.Exec(
		"INSERT INTO run_events(id, user_id, source, created_at) VALUES(?, ?, ?, ?)",
		"old-row-1", u, "chat", "2024-01-15 10:00:00",
	); err != nil {
		t.Fatalf("backfill old row: %v", err)
	}

	if n, _ := db.CountMonthlyAgentRuns(u); n != 5 {
		t.Errorf("previous-month row leaked into current count: want 5, got %d", n)
	}
}

func TestAgentRuns_UnknownSource(t *testing.T) {
	db, err := InitDB(t.TempDir())
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer db.Close()

	// Empty source should get normalized to "unknown" so counts still work
	if err := db.RecordAgentRun("u", ""); err != nil {
		t.Fatalf("RecordAgentRun with empty source: %v", err)
	}
	bySource, _ := db.CountDailyAgentRunsBySource("u")
	if bySource["unknown"] != 1 {
		t.Errorf("expected 1 'unknown' run, got %d (full map: %+v)", bySource["unknown"], bySource)
	}
}

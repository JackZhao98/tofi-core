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

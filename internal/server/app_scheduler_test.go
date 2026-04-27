package server

import (
	"testing"

	"tofi-core/internal/storage"
)

func TestActivateAppIsIdempotent(t *testing.T) {
	root := t.TempDir()
	db, err := storage.InitDB(root)
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}

	app := &storage.AppRecord{
		ID:               "soa-monitor",
		Name:             "SOA Monitor",
		ScheduleRules:    `{"entries":[{"time":"06:00","end_time":"23:30","interval_min":15,"repeat":{"type":"daily"},"enabled":true}],"timezone":"America/Los_Angeles"}`,
		BufferSize:       20,
		RenewalThreshold: 5,
		IsActive:         true,
		UserID:           "user-1",
		Skills:           "[]",
	}
	if err := db.CreateApp(app); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}

	scheduler := NewAppScheduler(&Server{db: db})
	if err := scheduler.ActivateApp(app); err != nil {
		t.Fatalf("first ActivateApp: %v", err)
	}
	firstCount, err := db.CountPendingAppRuns(app.ID)
	if err != nil {
		t.Fatalf("CountPendingAppRuns: %v", err)
	}
	if firstCount != 20 {
		t.Fatalf("expected 20 pending runs after first activation, got %d", firstCount)
	}

	if err := scheduler.ActivateApp(app); err != nil {
		t.Fatalf("second ActivateApp: %v", err)
	}
	secondCount, err := db.CountPendingAppRuns(app.ID)
	if err != nil {
		t.Fatalf("CountPendingAppRuns after second activation: %v", err)
	}
	if secondCount != firstCount {
		t.Fatalf("expected second activation to be idempotent, got %d pending runs (want %d)", secondCount, firstCount)
	}
}

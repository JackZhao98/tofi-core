package workspace

import (
	"testing"

	"tofi-core/internal/apps"
	"tofi-core/internal/storage"
)

func TestSyncAgentToDBPreservesRuntimeState(t *testing.T) {
	homeDir := t.TempDir()
	db, err := storage.InitDB(homeDir)
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}

	ws := New(homeDir)
	syncer := NewSync(ws, db)
	userID := "user-sync"

	def := &apps.AgentDef{
		Config: apps.AppConfig{
			ID:          "soa-monitor",
			Name:        "SOA Monitor",
			Description: "watch registration",
		},
		AgentsMD: "check the page",
	}
	if err := ws.WriteAgent(userID, def); err != nil {
		t.Fatalf("WriteAgent: %v", err)
	}

	record, err := syncer.SyncAgentToDB(userID, "soa-monitor")
	if err != nil {
		t.Fatalf("initial SyncAgentToDB: %v", err)
	}

	record.IsActive = true
	record.Parameters = `{"exam":"gh101"}`
	if err := db.UpdateApp(record); err != nil {
		t.Fatalf("UpdateApp: %v", err)
	}

	if _, err := syncer.SyncAgentToDB(userID, "soa-monitor"); err != nil {
		t.Fatalf("second SyncAgentToDB: %v", err)
	}

	got, err := db.GetApp("soa-monitor")
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	if !got.IsActive {
		t.Fatalf("expected IsActive to be preserved")
	}
	if got.Parameters != `{"exam":"gh101"}` {
		t.Fatalf("expected Parameters to be preserved, got %q", got.Parameters)
	}
}

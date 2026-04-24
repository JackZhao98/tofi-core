package server

import (
	"fmt"

	"tofi-core/internal/executor"
)

const (
	// quotaWarnThresholdPct is the disk-usage percentage at which the AI
	// starts seeing a hint in install tool results. Below this value the
	// meta block is silent.
	quotaWarnThresholdPct = 80
)

// quotaGate adapts the server's DB + settings to the executor.QuotaGate
// interface. One instance per server; userID is passed per-call.
type quotaGate struct {
	s *Server
}

// QuotaGate returns a lazy view of the server's quota logic. Safe to call
// for every request — it does not hold per-request state.
func (s *Server) QuotaGate() executor.QuotaGate {
	return &quotaGate{s: s}
}

func (g *quotaGate) Decide(userID string) executor.QuotaDecision {
	quota := g.s.toolRuntimeQuota("tool_runtime_user_quota_bytes", userID, defaultUserToolQuotaBytes)
	used, err := g.s.db.GetToolRuntimeTotalBytes("user", userID)
	if err != nil || quota <= 0 {
		return executor.QuotaDecision{Mode: executor.InstallModePersistent}
	}

	pct := int(used * 100 / quota)
	d := executor.QuotaDecision{DiskUsagePct: pct}
	switch {
	case pct >= 100:
		d.Mode = executor.InstallModeEphemeral
		d.Hint = "Disk quota full — installs this run will not persist after teardown."
	case pct >= quotaWarnThresholdPct:
		d.Mode = executor.InstallModePersistent
		d.Hint = fmt.Sprintf("Disk usage %d%% of quota. Consider calling tofi_disk_cleanup to free space.", pct)
	default:
		d.Mode = executor.InstallModePersistent
	}
	return d
}

func (g *quotaGate) BumpInventory(userID string) {
	g.s.bumpUserInventory(userID)
}

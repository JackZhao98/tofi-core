package server

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"tofi-core/internal/agent"
	"tofi-core/internal/provider"
)

// BuildDiskTools constructs the three deferred tools the AI can use to
// inspect and reclaim its user-scoped disk quota. They are registered
// per-request so the handler closes over the correct userID.
//
// All three are deferred — they are invisible until tofi_tool_search
// surfaces them (keywords like "disk", "quota", "cleanup", "storage").
func (s *Server) BuildDiskTools(userID string) []agent.ExtraBuiltinTool {
	if userID == "" {
		return nil
	}
	return []agent.ExtraBuiltinTool{
		s.buildDiskUsageTool(userID),
		s.buildDiskCleanupTool(userID),
	}
}

func (s *Server) buildDiskUsageTool(userID string) agent.ExtraBuiltinTool {
	return agent.ExtraBuiltinTool{
		Schema: provider.Tool{
			Name: "tofi_disk_usage",
			Description: "Show current disk usage and quota for your sandbox, " +
				"including the largest installed packages. Call this when you " +
				"see a near-quota hint in tool results and want to decide what " +
				"to clean up.",
			Parameters: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
				"required":   []string{},
			},
		},
		Deferred: true,
		Hint:     "disk quota usage storage space full size",
		Handler: func(_ map[string]interface{}) (string, error) {
			return s.diskUsageReport(userID)
		},
	}
}

func (s *Server) buildDiskCleanupTool(userID string) agent.ExtraBuiltinTool {
	return agent.ExtraBuiltinTool{
		Schema: provider.Tool{
			Name: "tofi_disk_cleanup",
			Description: "Delete named inventory items (venvs, npm packages, " +
				"uv tools, artifacts, bin entries) from your persistent storage " +
				"to free quota. Names come from tofi_disk_usage 'items' list. " +
				"Deleted items will be re-installed on next use if still needed.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"items": map[string]interface{}{
						"type":        "array",
						"description": "Display paths from tofi_disk_usage (e.g. 'python/venvs/default', 'npm/pandas').",
						"items":       map[string]interface{}{"type": "string"},
					},
				},
				"required": []string{"items"},
			},
		},
		Deferred: true,
		Hint:     "disk cleanup delete remove quota free space",
		Handler: func(args map[string]interface{}) (string, error) {
			raw, _ := args["items"].([]interface{})
			if len(raw) == 0 {
				return "No items specified.", nil
			}
			names := make([]string, 0, len(raw))
			for _, it := range raw {
				if name, ok := it.(string); ok && name != "" {
					names = append(names, name)
				}
			}
			return s.diskCleanup(userID, names)
		},
	}
}

// diskUsageReport returns a human+machine-readable text block with the
// user's quota status and per-item breakdown sorted by size (desc).
func (s *Server) diskUsageReport(userID string) (string, error) {
	summary, err := s.buildUserToolRuntimeSummary(userID)
	if err != nil {
		return "", fmt.Errorf("build usage: %w", err)
	}

	sort.Slice(summary.Items, func(i, j int) bool {
		return summary.Items[i].SizeBytes > summary.Items[j].SizeBytes
	})

	pct := 0
	if summary.QuotaBytes > 0 {
		pct = int(summary.TotalBytes * 100 / summary.QuotaBytes)
	}

	report := map[string]any{
		"used_bytes":    summary.TotalBytes,
		"quota_bytes":   summary.QuotaBytes,
		"usage_pct":     pct,
		"item_count":    summary.ItemCount,
		"by_category":   summary.ByCategory,
		"last_active":   summary.LastActiveAt,
		"top_items":     topItemsList(summary.Items, 20),
	}
	raw, _ := json.MarshalIndent(report, "", "  ")
	return string(raw), nil
}

// diskCleanup deletes each requested item. Refuses any path that
// escapes the user's own directory tree — so even an AI hallucinating
// a full host path can't rm something it shouldn't.
func (s *Server) diskCleanup(userID string, displayPaths []string) (string, error) {
	userRoot := filepath.Join(s.config.HomeDir, "users", userID)
	absRoot, err := filepath.Abs(userRoot)
	if err != nil {
		return "", fmt.Errorf("resolve user root: %w", err)
	}

	var (
		removed      []string
		skipped      []string
		freedBytes   int64
	)
	for _, rel := range displayPaths {
		target := filepath.Join(userRoot, rel)
		abs, err := filepath.Abs(target)
		if err != nil {
			skipped = append(skipped, rel+" (resolve failed)")
			continue
		}
		if !strings.HasPrefix(abs, absRoot+string(filepath.Separator)) && abs != absRoot {
			skipped = append(skipped, rel+" (outside user root)")
			continue
		}
		if abs == absRoot {
			skipped = append(skipped, rel+" (would delete entire user root)")
			continue
		}

		info, err := os.Stat(abs)
		if err != nil {
			skipped = append(skipped, rel+" (not found)")
			continue
		}
		size := info.Size()
		if info.IsDir() {
			size = treeSize(abs)
		}
		if err := os.RemoveAll(abs); err != nil {
			skipped = append(skipped, rel+" ("+err.Error()+")")
			continue
		}
		removed = append(removed, rel)
		freedBytes += size
	}

	go s.bumpUserInventory(userID)

	var b strings.Builder
	fmt.Fprintf(&b, "Freed %.1f MB (%d items).\n", float64(freedBytes)/(1024*1024), len(removed))
	if len(removed) > 0 {
		fmt.Fprintf(&b, "Removed:\n")
		for _, r := range removed {
			fmt.Fprintf(&b, "  - %s\n", r)
		}
	}
	if len(skipped) > 0 {
		fmt.Fprintf(&b, "Skipped:\n")
		for _, s := range skipped {
			fmt.Fprintf(&b, "  - %s\n", s)
		}
	}
	return b.String(), nil
}

func topItemsList(items []ToolRuntimeItemMeta, n int) []map[string]any {
	if len(items) < n {
		n = len(items)
	}
	out := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, map[string]any{
			"display_path": items[i].DisplayPath,
			"category":     items[i].Category,
			"size_bytes":   items[i].SizeBytes,
			"last_used_at": items[i].LastUsedAt,
		})
	}
	return out
}

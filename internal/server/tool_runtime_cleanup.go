package server

import (
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"tofi-core/internal/storage"
)

const (
	toolCleanupInterval         = 6 * time.Hour
	defaultUserToolQuotaBytes   = int64(1 << 30)   // 1 GB
	defaultSharedToolQuotaBytes = int64(50 << 30)  // 50 GB
	defaultToolRuntimeRetention = 7 * 24 * time.Hour
)

// startToolRuntimeCleanup launches a periodic cleanup for shared artifacts/caches
// and user-private tool/runtime directories. The first version intentionally uses
// filesystem timestamps instead of DB-backed reference counting so installations
// automatically stay bounded on a single-node deployment.
func (s *Server) startToolRuntimeCleanup() {
	go func() {
		s.cleanToolRuntimeFiles()

		ticker := time.NewTicker(toolCleanupInterval)
		defer ticker.Stop()
		for range ticker.C {
			s.cleanToolRuntimeFiles()
		}
	}()
}

func (s *Server) cleanToolRuntimeFiles() {
	now := time.Now()
	retention := s.toolRuntimeRetention()

	sharedRemoved, sharedBytes := cleanOldTree(filepath.Join(s.config.HomeDir, "packages", "artifacts"), now, retention)
	cacheRemovedA, cacheBytesA := cleanOldTree(filepath.Join(s.config.HomeDir, "packages", "cache", "pip"), now, retention)
	cacheRemovedB, cacheBytesB := cleanOldTree(filepath.Join(s.config.HomeDir, "packages", "cache", "uv"), now, retention)
	cacheRemovedC, cacheBytesC := cleanOldTree(filepath.Join(s.config.HomeDir, "packages", "cache", "npm"), now, retention)

	userRemoved, userBytes := 0, int64(0)
	usersDir := filepath.Join(s.config.HomeDir, "users")
	entries, err := os.ReadDir(usersDir)
	if err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			userRoot := filepath.Join(usersDir, entry.Name())
			for _, sub := range []string{
				filepath.Join(userRoot, "bin"),
				filepath.Join(userRoot, "python", "venvs"),
				filepath.Join(userRoot, "npm"),
				filepath.Join(userRoot, "uv", "tools"),
				filepath.Join(userRoot, "artifacts"),
				filepath.Join(userRoot, "state", "tool-runs"),
			} {
				removed, bytes := cleanOldTree(sub, now, retention)
				userRemoved += removed
				userBytes += bytes
			}
		}
	}

	totalRemoved := sharedRemoved + cacheRemovedA + cacheRemovedB + cacheRemovedC + userRemoved
	totalBytes := sharedBytes + cacheBytesA + cacheBytesB + cacheBytesC + userBytes
	if totalRemoved > 0 {
		log.Printf("[cleanup] Removed %d expired tool/runtime items (%.1f MB)", totalRemoved, float64(totalBytes)/(1024*1024))
	}

	sharedQuota := s.toolRuntimeQuota("tool_runtime_shared_quota_bytes", "", defaultSharedToolQuotaBytes)
	quotaRemoved, quotaBytes := evictOldestUntilUnderQuota(sharedQuota,
		filepath.Join(s.config.HomeDir, "packages", "artifacts"),
		filepath.Join(s.config.HomeDir, "packages", "cache", "pip"),
		filepath.Join(s.config.HomeDir, "packages", "cache", "uv"),
		filepath.Join(s.config.HomeDir, "packages", "cache", "npm"),
	)
	if quotaRemoved > 0 {
		log.Printf("[cleanup] Evicted %d shared tool/runtime items by quota (%.1f MB)", quotaRemoved, float64(quotaBytes)/(1024*1024))
	}

	if err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			userID := entry.Name()
			userRoot := filepath.Join(usersDir, userID)
			userQuota := s.toolRuntimeQuota("tool_runtime_user_quota_bytes", userID, defaultUserToolQuotaBytes)
			removed, bytes := evictOldestUntilUnderQuota(userQuota,
				filepath.Join(userRoot, "bin"),
				filepath.Join(userRoot, "python", "venvs"),
				filepath.Join(userRoot, "npm"),
				filepath.Join(userRoot, "uv", "tools"),
				filepath.Join(userRoot, "artifacts"),
			)
			if removed > 0 {
				log.Printf("[cleanup] Evicted %d user tool/runtime items for %s by quota (%.1f MB)", removed, userID, float64(bytes)/(1024*1024))
			}
		}
	}

	s.syncToolRuntimeInventory()
}

func (s *Server) toolRuntimeQuota(key, scope string, fallback int64) int64 {
	val, err := s.db.GetSetting(key, scope)
	if err != nil || val == "" {
		return fallback
	}
	n, err := strconv.ParseInt(val, 10, 64)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

func (s *Server) toolRuntimeRetention() time.Duration {
	val, err := s.db.GetSetting("tool_runtime_retention_hours", "")
	if err != nil || val == "" {
		return defaultToolRuntimeRetention
	}
	hours, err := strconv.ParseInt(val, 10, 64)
	if err != nil || hours <= 0 {
		return defaultToolRuntimeRetention
	}
	return time.Duration(hours) * time.Hour
}

// cleanOldTree removes immediate child files/directories whose modtime is older
// than maxAge. Directories are removed recursively and counted as one item.
func cleanOldTree(root string, now time.Time, maxAge time.Duration) (int, int64) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return 0, 0
	}

	removed := 0
	var reclaimed int64
	for _, entry := range entries {
		path := filepath.Join(root, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if now.Sub(info.ModTime()) <= maxAge {
			continue
		}
		size := treeSize(path)
		if err := os.RemoveAll(path); err != nil {
			continue
		}
		removed++
		reclaimed += size
	}
	return removed, reclaimed
}

func treeSize(path string) int64 {
	info, err := os.Lstat(path)
	if err != nil {
		return 0
	}
	if !info.IsDir() {
		return info.Size()
	}

	var total int64
	_ = filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil || info == nil {
			return nil
		}
		if !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total
}

type runtimeEntry struct {
	path    string
	modTime time.Time
	size    int64
}

func evictOldestUntilUnderQuota(quotaBytes int64, roots ...string) (int, int64) {
	if quotaBytes <= 0 {
		return 0, 0
	}

	var entries []runtimeEntry
	var total int64
	for _, root := range roots {
		rootEntries := collectImmediateChildren(root)
		entries = append(entries, rootEntries...)
		for _, entry := range rootEntries {
			total += entry.size
		}
	}
	if total <= quotaBytes {
		return 0, 0
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].modTime.Before(entries[j].modTime)
	})

	removed := 0
	var reclaimed int64
	for _, entry := range entries {
		if total <= quotaBytes {
			break
		}
		if err := os.RemoveAll(entry.path); err != nil {
			continue
		}
		total -= entry.size
		reclaimed += entry.size
		removed++
	}
	return removed, reclaimed
}

func collectImmediateChildren(root string) []runtimeEntry {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}

	var result []runtimeEntry
	for _, entry := range entries {
		path := filepath.Join(root, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}
		result = append(result, runtimeEntry{
			path:    path,
			modTime: info.ModTime(),
			size:    treeSize(path),
		})
	}
	return result
}

func collectToolRuntimeItems(category, root string) ([]ToolRuntimeItemMeta, int64, time.Time) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, 0, time.Time{}
	}

	var (
		items      []ToolRuntimeItemMeta
		totalBytes int64
		lastActive time.Time
	)
	for _, entry := range entries {
		path := filepath.Join(root, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}
		size := treeSize(path)
		lastUsed := info.ModTime().UTC()
		if lastUsed.After(lastActive) {
			lastActive = lastUsed
		}
		totalBytes += size
		items = append(items, ToolRuntimeItemMeta{
			Category:    category,
			Name:        entry.Name(),
			DisplayPath: displayToolRuntimePath(category, entry.Name()),
			IsDir:       info.IsDir(),
			SizeBytes:   size,
			FileType:    classifyToolRuntimeType(category, entry.Name(), info.IsDir()),
			LastUsedAt:  lastUsed.Format(time.RFC3339),
		})
	}
	return items, totalBytes, lastActive
}

// bumpUserInventory rescans a single user's directories and writes a fresh
// inventory snapshot to SQLite. Cheap enough to run after each install
// (typical ~50-200ms for 1GB quota) and keeps the quota gate reading
// accurate numbers without waiting for the 6h sweep.
//
// Safe to call from a goroutine — errors are logged and swallowed.
func (s *Server) bumpUserInventory(userID string) {
	if userID == "" {
		return
	}
	userRoot := filepath.Join(s.config.HomeDir, "users", userID)
	items := scanRuntimeInventory("user", userID, map[string]string{
		"bin":      filepath.Join(userRoot, "bin"),
		"venv":     filepath.Join(userRoot, "python", "venvs"),
		"npm":      filepath.Join(userRoot, "npm"),
		"uv_tool":  filepath.Join(userRoot, "uv", "tools"),
		"artifact": filepath.Join(userRoot, "artifacts"),
	})
	if err := s.db.ReplaceToolRuntimeInventory("user", userID, items); err != nil {
		log.Printf("[quota] bumpUserInventory(%s) failed: %v", userID, err)
	}
}

func (s *Server) syncToolRuntimeInventory() {
	sharedItems := scanRuntimeInventory("shared", "shared", map[string]string{
		"artifacts": filepath.Join(s.config.HomeDir, "packages", "artifacts"),
		"pip_cache": filepath.Join(s.config.HomeDir, "packages", "cache", "pip"),
		"uv_cache":  filepath.Join(s.config.HomeDir, "packages", "cache", "uv"),
		"npm_cache": filepath.Join(s.config.HomeDir, "packages", "cache", "npm"),
	})
	if err := s.db.ReplaceToolRuntimeInventory("shared", "shared", sharedItems); err != nil {
		log.Printf("[cleanup] tool runtime inventory sync failed for shared scope: %v", err)
	}

	usersDir := filepath.Join(s.config.HomeDir, "users")
	entries, err := os.ReadDir(usersDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		userID := entry.Name()
		userRoot := filepath.Join(usersDir, userID)
		items := scanRuntimeInventory("user", userID, map[string]string{
			"bin":      filepath.Join(userRoot, "bin"),
			"venv":     filepath.Join(userRoot, "python", "venvs"),
			"npm":      filepath.Join(userRoot, "npm"),
			"uv_tool":  filepath.Join(userRoot, "uv", "tools"),
			"artifact": filepath.Join(userRoot, "artifacts"),
		})
		if err := s.db.ReplaceToolRuntimeInventory("user", userID, items); err != nil {
			log.Printf("[cleanup] tool runtime inventory sync failed for %s: %v", userID, err)
		}
	}
}

func scanRuntimeInventory(scopeType, ownerID string, dirs map[string]string) []storage.ToolRuntimeInventoryItem {
	var items []storage.ToolRuntimeInventoryItem
	for category, root := range dirs {
		children, _, _ := collectToolRuntimeItems(category, root)
		for _, child := range children {
			items = append(items, storage.ToolRuntimeInventoryItem{
				ID:          scopeType + ":" + ownerID + ":" + child.Category + ":" + child.DisplayPath,
				ScopeType:   scopeType,
				OwnerID:     ownerID,
				Category:    child.Category,
				Name:        child.Name,
				DisplayPath: child.DisplayPath,
				FileType:    child.FileType,
				SizeBytes:   child.SizeBytes,
				IsDir:       child.IsDir,
				LastUsedAt:  child.LastUsedAt,
			})
		}
	}
	return items
}

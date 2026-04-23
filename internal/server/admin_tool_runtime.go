package server

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"tofi-core/internal/storage"
)

type ToolRuntimeSettingsResponse struct {
	SharedQuotaBytes      int64 `json:"shared_quota_bytes"`
	DefaultUserQuotaBytes int64 `json:"default_user_quota_bytes"`
	RetentionHours        int64 `json:"retention_hours"`
}

type ToolRuntimeUsageSummary struct {
	Username         string                 `json:"username"`
	QuotaBytes       int64                  `json:"quota_bytes"`
	TotalBytes       int64                  `json:"total_bytes"`
	ByCategory       map[string]int64       `json:"by_category"`
	ItemCount        int                    `json:"item_count"`
	LastActiveAt     string                 `json:"last_active_at,omitempty"`
	Items            []ToolRuntimeItemMeta  `json:"items"`
}

type ToolRuntimeItemMeta struct {
	Category     string `json:"category"`
	Name         string `json:"name"`
	DisplayPath  string `json:"display_path"`
	IsDir        bool   `json:"is_dir"`
	SizeBytes    int64  `json:"size_bytes"`
	FileType     string `json:"file_type"`
	LastUsedAt   string `json:"last_used_at,omitempty"`
}

func (s *Server) handleAdminGetToolRuntimeSettings(w http.ResponseWriter, r *http.Request) {
	resp := ToolRuntimeSettingsResponse{
		SharedQuotaBytes:      s.toolRuntimeQuota("tool_runtime_shared_quota_bytes", "", defaultSharedToolQuotaBytes),
		DefaultUserQuotaBytes: s.toolRuntimeQuota("tool_runtime_user_quota_bytes", "", defaultUserToolQuotaBytes),
		RetentionHours:        int64(s.toolRuntimeRetention() / time.Hour),
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleAdminSetToolRuntimeSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SharedQuotaBytes      *int64 `json:"shared_quota_bytes"`
		DefaultUserQuotaBytes *int64 `json:"default_user_quota_bytes"`
		RetentionHours        *int64 `json:"retention_hours"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, ErrBadRequest, "invalid request body", "")
		return
	}

	if req.SharedQuotaBytes != nil {
		if *req.SharedQuotaBytes <= 0 {
			writeJSONError(w, http.StatusBadRequest, ErrBadRequest, "shared_quota_bytes must be > 0", "")
			return
		}
		if err := s.db.SetSetting("tool_runtime_shared_quota_bytes", "system", strconv.FormatInt(*req.SharedQuotaBytes, 10)); err != nil {
			writeJSONError(w, http.StatusInternalServerError, ErrInternal, err.Error(), "")
			return
		}
	}
	if req.DefaultUserQuotaBytes != nil {
		if *req.DefaultUserQuotaBytes <= 0 {
			writeJSONError(w, http.StatusBadRequest, ErrBadRequest, "default_user_quota_bytes must be > 0", "")
			return
		}
		if err := s.db.SetSetting("tool_runtime_user_quota_bytes", "system", strconv.FormatInt(*req.DefaultUserQuotaBytes, 10)); err != nil {
			writeJSONError(w, http.StatusInternalServerError, ErrInternal, err.Error(), "")
			return
		}
	}
	if req.RetentionHours != nil {
		if *req.RetentionHours <= 0 {
			writeJSONError(w, http.StatusBadRequest, ErrBadRequest, "retention_hours must be > 0", "")
			return
		}
		if err := s.db.SetSetting("tool_runtime_retention_hours", "system", strconv.FormatInt(*req.RetentionHours, 10)); err != nil {
			writeJSONError(w, http.StatusInternalServerError, ErrInternal, err.Error(), "")
			return
		}
	}

	s.handleAdminGetToolRuntimeSettings(w, r)
}

func (s *Server) handleAdminGetToolRuntimeSummary(w http.ResponseWriter, r *http.Request) {
	s.syncToolRuntimeInventory()
	resp := map[string]any{
		"shared_quota_bytes": s.toolRuntimeQuota("tool_runtime_shared_quota_bytes", "", defaultSharedToolQuotaBytes),
		"shared_usage_bytes": s.sharedToolRuntimeSummary(),
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleAdminGetUserToolRuntime(w http.ResponseWriter, r *http.Request) {
	s.syncToolRuntimeInventory()
	username := r.PathValue("username")
	if username == "" {
		writeJSONError(w, http.StatusBadRequest, ErrBadRequest, "username required", "")
		return
	}
	user, err := s.db.GetUser(username)
	if err != nil || user == nil {
		writeJSONError(w, http.StatusNotFound, ErrNotFound, "user not found", "")
		return
	}

	summary, err := s.buildUserToolRuntimeSummary(username)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, ErrInternal, err.Error(), "")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(summary)
}

func (s *Server) handleAdminSetUserToolRuntimeQuota(w http.ResponseWriter, r *http.Request) {
	username := r.PathValue("username")
	if username == "" {
		writeJSONError(w, http.StatusBadRequest, ErrBadRequest, "username required", "")
		return
	}
	user, err := s.db.GetUser(username)
	if err != nil || user == nil {
		writeJSONError(w, http.StatusNotFound, ErrNotFound, "user not found", "")
		return
	}

	var req struct {
		QuotaBytes *int64 `json:"quota_bytes"`
		Clear      bool   `json:"clear"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, ErrBadRequest, "invalid request body", "")
		return
	}

	if req.Clear {
		if err := s.db.DeleteSetting("tool_runtime_user_quota_bytes", username); err != nil {
			writeJSONError(w, http.StatusInternalServerError, ErrInternal, err.Error(), "")
			return
		}
	} else {
		if req.QuotaBytes == nil || *req.QuotaBytes <= 0 {
			writeJSONError(w, http.StatusBadRequest, ErrBadRequest, "quota_bytes must be > 0", "")
			return
		}
		if err := s.db.SetSetting("tool_runtime_user_quota_bytes", username, strconv.FormatInt(*req.QuotaBytes, 10)); err != nil {
			writeJSONError(w, http.StatusInternalServerError, ErrInternal, err.Error(), "")
			return
		}
	}

	s.syncToolRuntimeInventory()
	s.handleAdminGetUserToolRuntime(w, r)
}

func (s *Server) buildUserToolRuntimeSummary(username string) (*ToolRuntimeUsageSummary, error) {
	resp := &ToolRuntimeUsageSummary{
		Username:   username,
		QuotaBytes: s.toolRuntimeQuota("tool_runtime_user_quota_bytes", username, defaultUserToolQuotaBytes),
		ByCategory: make(map[string]int64),
	}

	items, err := s.db.ListToolRuntimeInventory("user", username)
	if err != nil {
		return nil, err
	}

	var lastActive time.Time
	for _, item := range items {
		resp.TotalBytes += item.SizeBytes
		resp.ByCategory[item.Category] += item.SizeBytes
		resp.Items = append(resp.Items, ToolRuntimeItemMeta{
			Category:    item.Category,
			Name:        item.Name,
			DisplayPath: item.DisplayPath,
			IsDir:       item.IsDir,
			SizeBytes:   item.SizeBytes,
			FileType:    item.FileType,
			LastUsedAt:  item.LastUsedAt,
		})
		if item.LastUsedAt != "" {
			if ts, err := time.Parse(time.RFC3339, item.LastUsedAt); err == nil && ts.After(lastActive) {
				lastActive = ts
			}
		}
	}

	sort.Slice(resp.Items, func(i, j int) bool {
		return resp.Items[i].LastUsedAt > resp.Items[j].LastUsedAt
	})
	resp.ItemCount = len(resp.Items)
	if !lastActive.IsZero() {
		resp.LastActiveAt = lastActive.UTC().Format(time.RFC3339)
	}
	return resp, nil
}

func displayToolRuntimePath(category, name string) string {
	switch category {
	case "bin":
		return filepath.Join("bin", name)
	case "venv":
		return filepath.Join("python", "venvs", name)
	case "npm":
		return filepath.Join("npm", name)
	case "uv_tool":
		return filepath.Join("uv", "tools", name)
	case "artifact":
		return filepath.Join("artifacts", name)
	default:
		return name
	}
}

func classifyToolRuntimeType(category, name string, isDir bool) string {
	switch category {
	case "venv":
		return "python-venv"
	case "npm":
		if isDir {
			return "npm-package-tree"
		}
		return "npm-file"
	case "uv_tool":
		return "uv-tool"
	case "artifact":
		ext := strings.TrimPrefix(filepath.Ext(name), ".")
		if ext == "" {
			if isDir {
				return "artifact-dir"
			}
			return "artifact"
		}
		return "artifact-" + ext
	case "bin":
		if isDir {
			return "tool-dir"
		}
		return "binary"
	default:
		if isDir {
			return "dir"
		}
		return "file"
	}
}

func (s *Server) augmentUserDetailWithToolRuntime(resp *UserDetailResponse, username string) {
	summary, err := s.buildUserToolRuntimeSummary(username)
	if err != nil || summary == nil {
		return
	}
	resp.ToolRuntimeBytes = summary.TotalBytes
	resp.ToolRuntimeQuotaBytes = summary.QuotaBytes
	resp.ToolRuntimeItemCount = summary.ItemCount
}

func (s *Server) sharedToolRuntimeSummary() map[string]int64 {
	return map[string]int64{
		"artifacts": inventoryCategorySize(s.db, "shared", "shared", "artifacts", s.config.HomeDir),
		"pip_cache": inventoryCategorySize(s.db, "shared", "shared", "pip_cache", s.config.HomeDir),
		"uv_cache":  inventoryCategorySize(s.db, "shared", "shared", "uv_cache", s.config.HomeDir),
		"npm_cache": inventoryCategorySize(s.db, "shared", "shared", "npm_cache", s.config.HomeDir),
	}
}

func inventoryCategorySize(db *storage.DB, scopeType, ownerID, category, homeDir string) int64 {
	items, err := db.ListToolRuntimeInventory(scopeType, ownerID)
	if err != nil {
		switch category {
		case "artifacts":
			return treeSize(filepath.Join(homeDir, "packages", "artifacts"))
		case "pip_cache":
			return treeSize(filepath.Join(homeDir, "packages", "cache", "pip"))
		case "uv_cache":
			return treeSize(filepath.Join(homeDir, "packages", "cache", "uv"))
		case "npm_cache":
			return treeSize(filepath.Join(homeDir, "packages", "cache", "npm"))
		default:
			return 0
		}
	}
	var total int64
	for _, item := range items {
		if item.Category == category {
			total += item.SizeBytes
		}
	}
	return total
}

package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"tofi-core/internal/models"
	"tofi-core/internal/skills"
	"tofi-core/internal/storage"

	"github.com/google/uuid"
)

// --- Skill API Handlers ---

// handleListSkills GET /api/v1/skills — 列出用户安装的所有 Skills
func (s *Server) handleListSkills(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(UserContextKey).(string)

	keyword := r.URL.Query().Get("q")

	var records []*storage.SkillRecord
	var err error

	if keyword != "" {
		records, err = s.db.SearchSkills(userID, keyword)
	} else {
		records, err = s.db.ListSkills(userID)
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if records == nil {
		records = []*storage.SkillRecord{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(records)
}

// handleGetSkill GET /api/v1/skills/{id} — 获取 Skill 详情
func (s *Server) handleGetSkill(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "skill id required", http.StatusBadRequest)
		return
	}

	skill, err := s.db.GetSkill(id)
	if err != nil {
		http.Error(w, "skill not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(skill)
}

// handleInstallSkill POST /api/v1/skills/install — 安装 Skill
//
// 统一安装入口，支持三种方式:
//
//	1. source: "local" + content  — 直接粘贴 SKILL.md 内容
//	2. source: "git" + url        — owner/repo@skill 或 Git URL（兼容 skills CLI 格式）
//	3. source 省略 + content      — 等同于 local
func (s *Server) handleInstallSkill(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(UserContextKey).(string)

	var req struct {
		Source  string `json:"source"`  // "local" | "git" | ""
		Content string `json:"content"` // SKILL.md 内容 (source=local)
		URL     string `json:"url"`     // owner/repo@skill 或 Git URL (source=git)
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	switch req.Source {
	case "local", "":
		if req.Content == "" {
			http.Error(w, "content is required for local install", http.StatusBadRequest)
			return
		}
		s.installFromContent(w, userID, req.Content, "local", "")

	case "git":
		if req.URL == "" {
			http.Error(w, "url is required for git install", http.StatusBadRequest)
			return
		}
		s.installFromSource(w, userID, req.URL)

	default:
		http.Error(w, fmt.Sprintf("unsupported source: %s", req.Source), http.StatusBadRequest)
	}
}

// installFromContent 从 SKILL.md 内容安装（本地粘贴）
func (s *Server) installFromContent(w http.ResponseWriter, userID, content, source, sourceURL string) {
	skillFile, err := skills.Parse([]byte(content))
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid SKILL.md: %v", err), http.StatusBadRequest)
		return
	}

	// 保存到本地文件系统
	localStore := skills.NewLocalStore(s.config.HomeDir)
	if err := localStore.SaveLocal(skillFile.Manifest.Name, content); err != nil {
		log.Printf("[skills] warning: failed to save to local store: %v", err)
	}

	// 保存到数据库
	record := s.buildSkillRecord(userID, skillFile, source, sourceURL)
	if err := s.db.SaveSkill(record); err != nil {
		http.Error(w, fmt.Sprintf("failed to save skill: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(record)
}

// installFromSource 从 source 字符串安装（支持 owner/repo@skill、Git URL 等）
// 兼容 skills CLI 的所有格式: owner/repo, owner/repo@skill, https://github.com/...
func (s *Server) installFromSource(w http.ResponseWriter, userID, source string) {
	localStore := skills.NewLocalStore(s.config.HomeDir)
	installer := skills.NewSkillInstaller(localStore)

	result, err := installer.Install(source)
	if err != nil {
		http.Error(w, fmt.Sprintf("install failed: %v", err), http.StatusBadRequest)
		return
	}

	// 保存所有发现的 skills 到数据库
	var records []*storage.SkillRecord
	for _, sf := range result.Skills {
		record := s.buildSkillRecord(userID, sf, string(result.Source.Type), result.Source.DisplayURL())
		if err := s.db.SaveSkill(record); err != nil {
			log.Printf("[skills] warning: failed to save skill %s: %v", sf.Manifest.Name, err)
			continue
		}
		records = append(records, record)
	}

	if len(records) == 0 {
		http.Error(w, "failed to save any skills to database", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)

	// 如果只安装了一个 skill，返回单个对象（保持向后兼容）
	if len(records) == 1 {
		json.NewEncoder(w).Encode(records[0])
	} else {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"installed": len(records),
			"skills":    records,
		})
	}
}

// buildSkillRecord 构建 SkillRecord 数据库记录
func (s *Server) buildSkillRecord(userID string, sf *models.SkillFile, source, sourceURL string) *storage.SkillRecord {
	manifest := sf.Manifest
	manifestJSON, _ := json.Marshal(manifest)

	return &storage.SkillRecord{
		ID:              fmt.Sprintf("%s/%s", userID, manifest.Name),
		Name:            manifest.Name,
		Description:     manifest.Description,
		Version:         "1.0",
		Source:          source,
		SourceURL:       sourceURL,
		ManifestJSON:    string(manifestJSON),
		Instructions:    sf.Body,
		HasScripts:      len(sf.ScriptDirs) > 0,
		RequiredSecrets: toJSON(manifest.RequiredEnvVars()),
		AllowedTools:    toJSON(manifest.AllowedToolsList()),
		UserID:          userID,
		InstalledAt:     time.Now().Format("2006-01-02 15:04:05"),
	}
}

// handleDeleteSkill DELETE /api/v1/skills/{id} — 卸载 Skill
func (s *Server) handleDeleteSkill(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(UserContextKey).(string)
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "skill id required", http.StatusBadRequest)
		return
	}

	// 先获取 skill 信息（用于清理本地文件）
	skill, err := s.db.GetSkill(id)
	if err == nil && skill != nil {
		localStore := skills.NewLocalStore(s.config.HomeDir)
		localStore.Remove(skill.Name) // 清理本地文件，忽略错误
	}

	if err := s.db.DeleteSkill(id, userID); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleRunSkill POST /api/v1/skills/{id}/run — 直接运行 Skill
func (s *Server) handleRunSkill(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(UserContextKey).(string)
	id := r.PathValue("id")

	var req struct {
		Prompt       string `json:"prompt"`         // 用户输入
		Model        string `json:"model"`          // 可选覆盖模型
		UseSystemKey bool   `json:"use_system_key"` // 使用系统 API Key
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Prompt == "" {
		http.Error(w, "prompt is required", http.StatusBadRequest)
		return
	}

	skill, err := s.db.GetSkill(id)
	if err != nil {
		http.Error(w, "skill not found", http.StatusNotFound)
		return
	}

	wf := buildSkillWorkflow(skill, req.Prompt, req.Model, req.UseSystemKey)

	uuidStr := uuid.New().String()[:4]
	execID := time.Now().Format("102150405") + "-" + uuidStr

	ctx := models.NewExecutionContext(execID, userID, s.config.HomeDir)
	ctx.SetWorkflowName(wf.Name)
	ctx.WorkflowID = wf.ID
	ctx.DB = s.db

	job := &WorkflowJob{
		ExecutionID: execID,
		Workflow:    wf,
		Context:     ctx,
		DB:          s.db,
	}

	if err := s.workerPool.Submit(job); err != nil {
		http.Error(w, fmt.Sprintf("failed to submit: %v", err), http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{
		"execution_id": execID,
		"skill_id":     id,
		"status":       "queued",
	})
}

// buildSkillWorkflow 构建用于执行 Skill 的临时工作流对象
func buildSkillWorkflow(skill *storage.SkillRecord, prompt, model string, useSystemKey bool) *models.Workflow {
	if model == "" {
		model = "claude-sonnet-4-20250514"
	}

	wfName := "skill-" + skill.Name

	return &models.Workflow{
		ID:          wfName + "_ephemeral",
		Name:        wfName,
		Description: "Auto-generated workflow for skill: " + skill.Name,
		Nodes: map[string]*models.Node{
			"run_skill": {
				ID:   "run_skill",
				Name: "Run " + skill.Name,
				Type: "skill",
				Config: map[string]interface{}{
					"skill_id":       skill.ID,
					"prompt":         prompt,
					"model":          model,
					"use_system_key": useSystemKey,
				},
			},
		},
	}
}

// --- Registry Handlers (搜索 skills.sh) ---

// handleRegistrySearch GET /api/v1/registry/search?q=xxx — 搜索 skills.sh
func (s *Server) handleRegistrySearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		http.Error(w, "search query 'q' is required", http.StatusBadRequest)
		return
	}

	client := skills.NewRegistryClient("")
	result, err := client.Search(query, 10)
	if err != nil {
		log.Printf("[registry] search failed: %v", err)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"skills": []interface{}{},
			"total":  0,
			"error":  err.Error(),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// --- Helper functions ---

func toJSON(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "[]"
	}
	return string(b)
}

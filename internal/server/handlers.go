package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
	"tofi-core/internal/crypto"
	"tofi-core/internal/models"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// --- Request/Response Structs ---

type SetupRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type CreateSecretRequest struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type SecretResponse struct {
	Name      string `json:"name"`
	CreatedAt string `json:"created_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
	Value     string `json:"value,omitempty"`
}

type SecretListResponse struct {
	Secrets []SecretResponse `json:"secrets"`
}

// --- Auth Handlers ---

func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	count, err := s.db.CountUsers()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, ErrInternal, err.Error(), "")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"initialized": count > 0})
}

func (s *Server) handleSetupAdmin(w http.ResponseWriter, r *http.Request) {
	count, _ := s.db.CountUsers()
	if count > 0 {
		writeJSONError(w, http.StatusForbidden, ErrForbidden, "System already initialized", "")
		return
	}

	var req SetupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, ErrBadRequest, "Invalid request", "")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, ErrInternal, "Failed to hash password", "")
		return
	}

	id := uuid.New().String()
	if err := s.db.SaveUser(id, req.Username, string(hash), "admin"); err != nil {
		writeJSONError(w, http.StatusInternalServerError, ErrInternal, err.Error(), "")
		return
	}

	w.WriteHeader(http.StatusCreated)
	fmt.Fprint(w, "Admin created successfully")
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, ErrBadRequest, "Invalid request body", "")
		return
	}

	user, err := s.db.GetUser(req.Username)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, ErrInvalidCredentials, "Invalid username or password", "")
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		writeJSONError(w, http.StatusUnauthorized, ErrInvalidCredentials, "Invalid username or password", "")
		return
	}

	// Block pending users if email verification is required
	if s.requireVerifiedEmail() && user.Status == "pending" {
		writeJSONError(w, http.StatusForbidden, "EMAIL_NOT_VERIFIED", "Please verify your email first", "POST /api/v1/auth/resend-code to get a new code")
		return
	}

	token, err := GenerateToken(user.Username, user.Role)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, ErrInternal, err.Error(), "")
		return
	}

	// Issue refresh token
	refreshToken, err := s.issueRefreshToken(user.Username)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, ErrInternal, err.Error(), "")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"token":         token,
		"refresh_token": refreshToken,
		"expires_in":    int(7 * 24 * time.Hour / time.Second), // 604800
		"username":      user.Username,
		"role":          user.Role,
	})
}

func (s *Server) handleGetMe(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value(UserContextKey).(string)
	u, err := s.db.GetUser(user)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, ErrNotFound, "User not found", "")
		return
	}

	// Build available providers — only include providers with a key configured
	allProviders := []struct {
		name         string
		defaultModel string
	}{
		{"openai", "gpt-5.4"},
		{"anthropic", "claude-sonnet-4-20250514"},
		{"gemini", "gemini-2.5-flash"},
		{"deepseek", "deepseek-chat"},
		{"groq", ""},
		{"openrouter", ""},
	}
	type providerStatus struct {
		Provider     string `json:"provider"`
		Source       string `json:"source"`        // "user" or "system"
		DefaultModel string `json:"default_model"` // default model for this provider
	}
	var available []providerStatus
	for _, p := range allProviders {
		if key, err := s.db.ResolveAIKey(p.name, user); err == nil && key != "" {
			source := "system"
			if userKey, _ := s.db.GetSecret(user, "ai_key_"+p.name); userKey != nil {
				source = "user"
			}
			available = append(available, providerStatus{
				Provider:     p.name,
				Source:       source,
				DefaultModel: p.defaultModel,
			})
		}
	}
	if available == nil {
		available = []providerStatus{}
	}

	resp := map[string]any{
		"username":  u.Username,
		"role":      u.Role,
		"providers": available,
	}
	// When no providers are configured, tell the user what's available
	if len(available) == 0 {
		resp["available_providers"] = []string{"openai", "anthropic", "gemini", "deepseek", "groq", "openrouter"}
		resp["hint"] = "No AI providers configured. Set a key: PUT /api/v1/user/settings/ai-key"
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// --- File Preview Handler ---

func (s *Server) handlePreviewFileGlobal(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value(UserContextKey).(string)
	id := r.PathValue("id")

	files, err := s.db.ListUserFiles(user)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, ErrInternal, "Failed to list files", "")
		return
	}

	var target *models.UserFileRecord
	for _, f := range files {
		if f.FileID == id || f.UUID == id {
			target = f
			break
		}
	}

	if target == nil {
		writeJSONError(w, http.StatusNotFound, ErrNotFound, "File not found", "")
		return
	}

	filePath := filepath.Join(s.config.HomeDir, user, "storage", "files", target.UUID)
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		writeJSONError(w, http.StatusNotFound, ErrNotFound, "File not found on disk", "")
		return
	}

	w.Header().Set("Content-Type", target.MimeType)
	w.Header().Set("Content-Disposition", "inline; filename=\""+target.OriginalFilename+"\"")
	http.ServeFile(w, r, filePath)
}

// --- Global File Library Handlers ---

func (s *Server) handleUploadFileGlobal(w http.ResponseWriter, r *http.Request) {
	// Parse max 100MB
	r.ParseMultipartForm(100 << 20)

	user := r.Context().Value(UserContextKey).(string)
	fileID := r.FormValue("file_id")

	if fileID == "" {
		writeJSONError(w, http.StatusBadRequest, ErrBadRequest, "file_id is required", "")
		return
	}

	// Check 1GB Quota
	currentSize, err := s.db.GetUserTotalFileSize(user)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, ErrInternal, "Failed to check quota", "")
		return
	}
	if currentSize >= 1*1024*1024*1024 { // 1GB
		writeJSONError(w, http.StatusForbidden, ErrForbidden, "Storage quota exceeded (1GB limit)", "")
		return
	}

	// Check file_id uniqueness
	exists, err := s.db.CheckFileIDExists(user, fileID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, ErrInternal, "Database error", "")
		return
	}
	if exists {
		writeJSONError(w, http.StatusConflict, ErrConflict, fmt.Sprintf("File ID '%s' already exists", fileID), "")
		return
	}

	file, handler, err := r.FormFile("file")
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, ErrBadRequest, "Failed to get file", "")
		return
	}
	defer file.Close()

	if handler.Size > 100*1024*1024 {
		writeJSONError(w, http.StatusRequestEntityTooLarge, ErrBadRequest, "File too large (max 100MB)", "")
		return
	}

	// Detect MIME type
	buff := make([]byte, 512)
	if _, err := file.Read(buff); err != nil {
		writeJSONError(w, http.StatusInternalServerError, ErrInternal, "Failed to read file", "")
		return
	}
	mimeType := http.DetectContentType(buff)
	file.Seek(0, 0) // Reset pointer

	uuidStr := uuid.New().String()
	storageDir := filepath.Join(s.config.HomeDir, user, "storage", "files")
	os.MkdirAll(storageDir, 0755)

	destPath := filepath.Join(storageDir, uuidStr)
	dest, err := os.Create(destPath)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, ErrInternal, "Failed to store file", "")
		return
	}
	defer dest.Close()

	if _, err := io.Copy(dest, file); err != nil {
		writeJSONError(w, http.StatusInternalServerError, ErrInternal, "Failed to write file", "")
		return
	}

	// Save metadata to DB
	err = s.db.SaveUserFile(
		uuidStr,
		fileID,
		user,
		handler.Filename,
		mimeType,
		handler.Size,
		"", // hash optional for now
	)
	if err != nil {
		// Clean up file if DB fails
		os.Remove(destPath)
		writeJSONError(w, http.StatusInternalServerError, ErrInternal, "Failed to save metadata", "")
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{
		"uuid":              uuidStr,
		"file_id":           fileID,
		"original_filename": handler.Filename,
	})
}

func (s *Server) handleListFilesGlobal(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value(UserContextKey).(string)
	files, err := s.db.ListUserFiles(user)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, ErrInternal, "Failed to list files", "")
		return
	}
	if files == nil {
		files = make([]*models.UserFileRecord, 0)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(files)
}

func (s *Server) handleDeleteFileGlobal(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value(UserContextKey).(string)
	id := r.PathValue("id")

	// Get file to find UUID (for physical deletion)
	files, _ := s.db.ListUserFiles(user)
	var target *models.UserFileRecord
	for _, f := range files {
		if f.FileID == id || f.UUID == id {
			target = f
			break
		}
	}

	if target == nil {
		writeJSONError(w, http.StatusNotFound, ErrNotFound, "File not found", "")
		return
	}

	// Delete from DB
	if err := s.db.DeleteUserFile(user, id); err != nil {
		writeJSONError(w, http.StatusInternalServerError, ErrInternal, "Failed to delete metadata", "")
		return
	}

	// Delete physical file
	path := filepath.Join(s.config.HomeDir, user, "storage", "files", target.UUID)
	os.Remove(path) // Ignore error if already gone

	w.WriteHeader(http.StatusNoContent)
}

// --- Secret Handlers ---

func (s *Server) handleCreateSecret(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value(UserContextKey).(string)
	var req CreateSecretRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, ErrBadRequest, "Invalid request", "")
		return
	}

	val, err := crypto.Encrypt(req.Value)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, ErrInternal, "Encryption failed", "")
		return
	}

	if err := s.db.SaveSecret(uuid.New().String(), user, req.Name, val); err != nil {
		writeJSONError(w, http.StatusInternalServerError, ErrInternal, err.Error(), "")
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func (s *Server) handleGetSecret(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value(UserContextKey).(string)
	name := r.PathValue("name")
	record, err := s.db.GetSecret(user, name)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, ErrNotFound, "Secret not found", "")
		return
	}
	val, _ := crypto.Decrypt(record.EncryptedValue)
	json.NewEncoder(w).Encode(SecretResponse{Name: record.Name, Value: val})
}

func (s *Server) handleListSecrets(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value(UserContextKey).(string)
	records, _ := s.db.ListSecrets(user)
	resp := []SecretResponse{}
	for _, r := range records {
		resp = append(resp, SecretResponse{Name: r.Name, CreatedAt: r.CreatedAt.String})
	}
	json.NewEncoder(w).Encode(SecretListResponse{Secrets: resp})
}

func (s *Server) handleDeleteSecret(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value(UserContextKey).(string)
	name := r.PathValue("name")
	s.db.DeleteSecret(user, name)
	w.WriteHeader(http.StatusNoContent)
}

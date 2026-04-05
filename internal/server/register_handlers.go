package server

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"os"
	"regexp"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// ── Admin Settings Helpers ──

func (s *Server) allowSignup() bool {
	val, _ := s.db.GetSetting("allow_signup", "system")
	return val == "true"
}

func (s *Server) requireVerifiedEmail() bool {
	val, _ := s.db.GetSetting("require_verified_email", "system")
	return val == "true"
}

func (s *Server) resendAPIKey() string {
	return os.Getenv("TOFI_RESEND_API_KEY")
}

// ── Registration Handlers ──

var emailRegex = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)

// handleRegister POST /api/v1/auth/register
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if !s.allowSignup() {
		writeJSONError(w, http.StatusForbidden, ErrForbidden, "Registration is disabled", "Contact the admin to create an account")
		return
	}

	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Username string `json:"username"` // optional — defaults to email prefix
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, ErrBadRequest, "Invalid request body", "")
		return
	}

	// Validate email
	if req.Email == "" || !emailRegex.MatchString(req.Email) {
		writeJSONError(w, http.StatusBadRequest, ErrBadRequest, "Valid email is required", "")
		return
	}

	// Validate password
	if len(req.Password) < 8 {
		writeJSONError(w, http.StatusBadRequest, ErrBadRequest, "Password must be at least 8 characters", "")
		return
	}

	// Default username to email prefix
	if req.Username == "" {
		req.Username = req.Email
	}

	// Global rate limit: 100 registrations/day
	todayCount, _ := s.db.CountTodayRegistrations()
	if todayCount >= 100 {
		writeJSONError(w, http.StatusTooManyRequests, ErrRateLimited, "Registration limit reached for today", "Try again tomorrow")
		return
	}

	// Check email uniqueness
	existing, _ := s.db.GetUserByEmail(req.Email)
	if existing != nil {
		writeJSONError(w, http.StatusConflict, ErrConflict, "Email already registered", "")
		return
	}

	// Check username uniqueness
	existingUser, _ := s.db.GetUser(req.Username)
	if existingUser != nil {
		writeJSONError(w, http.StatusConflict, ErrConflict, "Username already taken", "")
		return
	}

	// Hash password
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, ErrInternal, "Failed to process password", "")
		return
	}

	// Determine initial status
	status := "active"
	if s.requireVerifiedEmail() {
		status = "pending"
	}

	// Create user
	userID := uuid.New().String()
	if err := s.db.SaveUserWithEmail(userID, req.Username, req.Email, string(hash), "user", status); err != nil {
		writeJSONError(w, http.StatusInternalServerError, ErrInternal, fmt.Sprintf("Failed to create user: %v", err), "")
		return
	}

	// If verification required, send code
	if status == "pending" {
		code := generateVerificationCode()
		expiresAt := time.Now().Add(15 * time.Minute).Format(time.RFC3339)
		s.db.SetVerificationCode(userID, code, expiresAt)

		if err := sendVerificationEmail(s.resendAPIKey(), req.Email, code); err != nil {
			log.Printf("[register] Failed to send verification email to %s: %v", req.Email, err)
			// Don't fail registration, user can resend
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "pending",
			"message": "Check your email for a verification code",
			"email":   req.Email,
		})
		return
	}

	// No verification needed — return token directly
	token, err := GenerateToken(req.Username, "user")
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, ErrInternal, "Failed to generate token", "")
		return
	}
	refreshToken, err := s.issueRefreshToken(req.Username)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, ErrInternal, "Failed to generate refresh token", "")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":        "active",
		"token":         token,
		"refresh_token": refreshToken,
		"expires_in":    int(7 * 24 * time.Hour / time.Second),
		"username":      req.Username,
		"role":          "user",
	})
}

// handleVerifyEmail POST /api/v1/auth/verify
func (s *Server) handleVerifyEmail(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
		Code  string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, ErrBadRequest, "Invalid request body", "")
		return
	}

	if req.Email == "" || req.Code == "" {
		writeJSONError(w, http.StatusBadRequest, ErrBadRequest, "Email and code are required", "")
		return
	}

	user, err := s.db.GetUserByEmail(req.Email)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, ErrUnauthorized, "Invalid email or code", "")
		return
	}

	// Already active — idempotent
	if user.Status == "active" {
		token, _ := GenerateToken(user.Username, user.Role)
		refreshToken, _ := s.issueRefreshToken(user.Username)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":        "active",
			"token":         token,
			"refresh_token": refreshToken,
			"expires_in":    int(7 * 24 * time.Hour / time.Second),
			"username":      user.Username,
			"role":          user.Role,
		})
		return
	}

	// Check code
	if user.VerificationCode != req.Code {
		writeJSONError(w, http.StatusUnauthorized, ErrUnauthorized, "Invalid verification code", "")
		return
	}

	// Check expiry
	if user.CodeExpiresAt != "" {
		expiresAt, err := time.Parse(time.RFC3339, user.CodeExpiresAt)
		if err == nil && time.Now().After(expiresAt) {
			writeJSONError(w, http.StatusUnauthorized, ErrUnauthorized, "Verification code has expired", "Use resend-code to get a new one")
			return
		}
	}

	// Activate user
	s.db.UpdateUserStatus(user.ID, "active")
	s.db.ClearVerificationCode(user.ID)

	// Return token
	token, err := GenerateToken(user.Username, user.Role)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, ErrInternal, "Failed to generate token", "")
		return
	}
	refreshToken, err := s.issueRefreshToken(user.Username)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, ErrInternal, "Failed to generate refresh token", "")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":        "active",
		"token":         token,
		"refresh_token": refreshToken,
		"expires_in":    int(7 * 24 * time.Hour / time.Second),
		"username":      user.Username,
		"role":          user.Role,
		"message":       "Email verified successfully",
	})
}

// handleResendCode POST /api/v1/auth/resend-code
func (s *Server) handleResendCode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, ErrBadRequest, "Invalid request body", "")
		return
	}

	// Always return 200 to prevent email enumeration
	w.Header().Set("Content-Type", "application/json")

	if req.Email == "" {
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "message": "If the email exists, a new code has been sent"})
		return
	}

	user, err := s.db.GetUserByEmail(req.Email)
	if err != nil || user == nil || user.Status == "active" {
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "message": "If the email exists, a new code has been sent"})
		return
	}

	code := generateVerificationCode()
	expiresAt := time.Now().Add(15 * time.Minute).Format(time.RFC3339)
	s.db.SetVerificationCode(user.ID, code, expiresAt)

	if err := sendVerificationEmail(s.resendAPIKey(), req.Email, code); err != nil {
		log.Printf("[resend-code] Failed to send email to %s: %v", req.Email, err)
	}

	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "message": "If the email exists, a new code has been sent"})
}

// ── Admin Registration Settings ──

// handleGetRegistrationSettings GET /api/v1/admin/settings/registration
func (s *Server) handleGetRegistrationSettings(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"allow_signup":           s.allowSignup(),
		"require_verified_email": s.requireVerifiedEmail(),
		"email_provider":         "resend",
		"email_configured":       s.resendAPIKey() != "",
	})
}

// handleSetRegistrationSettings PUT /api/v1/admin/settings/registration
func (s *Server) handleSetRegistrationSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AllowSignup          *bool `json:"allow_signup"`
		RequireVerifiedEmail *bool `json:"require_verified_email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, ErrBadRequest, "Invalid request body", "")
		return
	}

	if req.AllowSignup != nil {
		val := "false"
		if *req.AllowSignup {
			val = "true"
		}
		s.db.SetSetting("allow_signup", "system", val)
	}
	if req.RequireVerifiedEmail != nil {
		val := "false"
		if *req.RequireVerifiedEmail {
			val = "true"
		}
		s.db.SetSetting("require_verified_email", "system", val)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":                 "updated",
		"allow_signup":           s.allowSignup(),
		"require_verified_email": s.requireVerifiedEmail(),
	})
}

// ── Helpers ──

func generateVerificationCode() string {
	code := ""
	for i := 0; i < 6; i++ {
		n, _ := rand.Int(rand.Reader, big.NewInt(10))
		code += fmt.Sprintf("%d", n.Int64())
	}
	return code
}

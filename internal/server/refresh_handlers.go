package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
	"tofi-core/internal/storage"
)

const refreshTokenExpiry = 30 * 24 * time.Hour // 30 days

// issueRefreshToken creates and stores a new refresh token for the user.
// Returns the raw token (to send to client) and any error.
func (s *Server) issueRefreshToken(userID string) (string, error) {
	tokenBody, tokenHash := storage.GenerateSecureToken(32) // 32 bytes = 64 hex chars
	id := uuid.New().String()
	expiresAt := time.Now().Add(refreshTokenExpiry)
	if err := s.db.CreateRefreshToken(id, userID, tokenHash, expiresAt); err != nil {
		return "", err
	}
	return tokenBody, nil
}

// handleRefreshToken exchanges a valid refresh token for a new access token.
// Implements rotation: old refresh token is consumed, new one is issued.
// POST /api/v1/auth/refresh (public, no auth required)
func (s *Server) handleRefreshToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RefreshToken == "" {
		writeJSONError(w, http.StatusBadRequest, ErrBadRequest, "refresh_token is required", "")
		return
	}

	// Hash the token and look up
	h := sha256.Sum256([]byte(req.RefreshToken))
	tokenHash := hex.EncodeToString(h[:])

	rec, err := s.db.GetRefreshTokenByHash(tokenHash)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, ErrInternal, err.Error(), "")
		return
	}
	if rec == nil {
		writeJSONError(w, http.StatusUnauthorized, ErrUnauthorized, "Invalid refresh token", "Login again: POST /api/v1/auth/login")
		return
	}

	// Check expiration
	expiresAt, err := time.Parse(time.RFC3339, rec.ExpiresAt)
	if err != nil || time.Now().After(expiresAt) {
		s.db.DeleteRefreshToken(rec.ID) // clean up expired token
		writeJSONError(w, http.StatusUnauthorized, ErrUnauthorized, "Refresh token has expired", "Login again: POST /api/v1/auth/login")
		return
	}

	// Rotation: delete old refresh token
	s.db.DeleteRefreshToken(rec.ID)

	// Look up user role
	user, err := s.db.GetUser(rec.UserID)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, ErrUnauthorized, "User not found", "")
		return
	}

	// Issue new access token
	accessToken, err := GenerateToken(rec.UserID, user.Role)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, ErrInternal, err.Error(), "")
		return
	}

	// Issue new refresh token (rotation)
	newRefreshToken, err := s.issueRefreshToken(rec.UserID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, ErrInternal, err.Error(), "")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"token":         accessToken,
		"refresh_token": newRefreshToken,
		"expires_in":    int(7 * 24 * time.Hour / time.Second), // 604800
	})
}

// handleRevokeTokens revokes all refresh tokens for the authenticated user.
// POST /api/v1/auth/revoke
func (s *Server) handleRevokeTokens(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(UserContextKey).(string)

	if err := s.db.DeleteAllRefreshTokens(userID); err != nil {
		writeJSONError(w, http.StatusInternalServerError, ErrInternal, err.Error(), "")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "all refresh tokens revoked"})
}

// handleAdminRevokeUserTokens revokes all refresh tokens for a specific user (admin).
// DELETE /api/v1/admin/users/{id}/tokens
func (s *Server) handleAdminRevokeUserTokens(w http.ResponseWriter, r *http.Request) {
	targetUserID := r.PathValue("id")

	if err := s.db.DeleteAllRefreshTokens(targetUserID); err != nil {
		writeJSONError(w, http.StatusInternalServerError, ErrInternal, err.Error(), "")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "all refresh tokens revoked for " + targetUserID})
}

// startRefreshTokenCleanup starts a goroutine that periodically cleans up expired refresh tokens.
func (s *Server) startRefreshTokenCleanup() {
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			if n, err := s.db.CleanupExpiredRefreshTokens(); err != nil {
				log.Printf("Refresh token cleanup error: %v", err)
			} else if n > 0 {
				log.Printf("Cleaned up %d expired refresh tokens", n)
			}
		}
	}()
}

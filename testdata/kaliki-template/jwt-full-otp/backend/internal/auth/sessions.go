package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	gen "github.com/example/jwt-full-otp-app/backend/gen/api/auth"
	"github.com/example/jwt-full-otp-app/backend/internal/database"
)

// respondWithSession issues a fresh access token + rotated refresh token and
// writes the AuthResponse.
func (s *Service) respondWithSession(c *gin.Context, status int, user database.User) {
	access, refresh, err := s.issueSession(c.Request.Context(), user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not issue session"})
		return
	}
	c.JSON(status, gen.AuthResponse{
		Token:        access,
		RefreshToken: refresh,
		User:         toAPIUser(user),
	})
}

func (s *Service) issueSession(ctx context.Context, user database.User) (access, refresh string, err error) {
	access, err = s.tokens.Issue(user.ID.String(), user.Role)
	if err != nil {
		return "", "", err
	}
	raw, hash := newSecret()
	if _, err = s.store.CreateRefreshToken(ctx, database.CreateRefreshTokenParams{
		UserID:    user.ID,
		TokenHash: hash,
		ExpiresAt: time.Now().Add(s.refreshTTL),
	}); err != nil {
		return "", "", err
	}
	return access, raw, nil
}

// RefreshToken implements gen.ServerInterface. Rotates the refresh token:
// the presented token is revoked and a new access/refresh pair is returned.
func (s *Service) RefreshToken(c *gin.Context) {
	var body gen.RefreshTokenJSONRequestBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	old, err := s.store.GetRefreshToken(c.Request.Context(), hashToken(body.RefreshToken))
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid refresh token"})
		return
	}
	_ = s.store.RevokeRefreshToken(c.Request.Context(), old.ID)

	user, err := s.store.GetUserByID(c.Request.Context(), old.UserID)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid refresh token"})
		return
	}
	if !user.Verified {
		// #14: refresh isn't a verification-prompt moment (unlike Login), so no
		// fresh credential is reissued here — just refuse the session.
		c.JSON(http.StatusForbidden, gin.H{"error": "email_not_verified"})
		return
	}
	s.respondWithSession(c, http.StatusOK, user)
}

// Logout implements gen.ServerInterface. Revokes the presented refresh token.
// Always returns 204 so it is safe to call with an already-invalid token.
func (s *Service) Logout(c *gin.Context) {
	var body gen.LogoutJSONRequestBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if tok, err := s.store.GetRefreshToken(c.Request.Context(), hashToken(body.RefreshToken)); err == nil {
		_ = s.store.RevokeRefreshToken(c.Request.Context(), tok.ID)
	}
	c.Status(http.StatusNoContent)
}

// GetCurrentUser implements gen.ServerInterface. A worked example of a
// JWT-protected endpoint: /auth/me declares `security: bearerAuth` in the spec,
// so ScopeAuth validated the token and set user_id on the context before this
// runs. The handler just reads it and returns the user record.
func (s *Service) GetCurrentUser(c *gin.Context) {
	id, err := uuid.Parse(c.GetString("user_id"))
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token subject"})
		return
	}
	user, err := s.store.GetUserByID(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not found"})
		return
	}
	c.JSON(http.StatusOK, toAPIUser(user))
}

func newSecret() (raw, hash string) {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	raw = hex.EncodeToString(b)
	return raw, hashToken(raw)
}

func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

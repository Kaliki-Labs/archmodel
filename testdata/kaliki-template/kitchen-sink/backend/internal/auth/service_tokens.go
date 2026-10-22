package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	gen "github.com/example/kitchen-sink-app/backend/gen/api/auth"
	"github.com/example/kitchen-sink-app/backend/internal/database"
)

const (
	kindVerify        = "verify"
	kindPasswordReset = "password_reset"
)

const (
	verifyTTL = 72 * time.Hour
	resetTTL  = time.Hour
)

// VerifyEmail implements gen.ServerInterface. Consumes a verification
// token, marks the account verified, and returns a fresh session.
func (s *Service) VerifyEmail(c *gin.Context) {
	var body gen.VerifyEmailJSONRequestBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tok, err := s.consume(c.Request.Context(), body.Token, kindVerify)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid or expired token"})
		return
	}

	if err := s.store.VerifyUser(c.Request.Context(), tok.UserID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not verify user"})
		return
	}
	_ = s.store.MarkAuthTokenUsed(c.Request.Context(), tok.ID)

	verifiedUser, err := s.store.GetUserByID(c.Request.Context(), tok.UserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load user"})
		return
	}
	s.respondWithSession(c, http.StatusOK, verifiedUser)
}

// RequestPasswordReset implements gen.ServerInterface. Always returns 200 so
// callers cannot probe which emails exist; the reset credential is emailed.
func (s *Service) RequestPasswordReset(c *gin.Context) {
	var body gen.RequestPasswordResetJSONRequestBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	user, err := s.store.GetUserByEmail(c.Request.Context(), string(body.Email))
	if err != nil {
		c.Status(http.StatusOK)
		return
	}
	credential, err := s.issueReset(c.Request.Context(), user.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not issue reset"})
		return
	}
	s.sendReset(c.Request.Context(), user.Email, credential)
	c.Status(http.StatusOK)
}

// ConfirmPasswordReset implements gen.ServerInterface.
func (s *Service) ConfirmPasswordReset(c *gin.Context) {
	var body gen.ConfirmPasswordResetJSONRequestBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := ValidatePassword(body.Password); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tok, err := s.consume(c.Request.Context(), body.Token, kindPasswordReset)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid or expired token"})
		return
	}

	hash, err := HashPassword(body.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not hash password"})
		return
	}
	if err := s.store.UpdateUserPassword(c.Request.Context(), tok.UserID, hash); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not update password"})
		return
	}
	_ = s.store.MarkAuthTokenUsed(c.Request.Context(), tok.ID)
	c.Status(http.StatusOK)
}

// issueVerification / issueReset create a credential, persist its hash, and
// return the raw value to be emailed.
func (s *Service) issueVerification(ctx context.Context, userID uuid.UUID) (string, error) {
	return s.issueCredential(ctx, userID, kindVerify, verifyTTL)
}

func (s *Service) issueReset(ctx context.Context, userID uuid.UUID) (string, error) {
	return s.issueCredential(ctx, userID, kindPasswordReset, resetTTL)
}

func (s *Service) issueCredential(ctx context.Context, userID uuid.UUID, kind string, ttl time.Duration) (string, error) {
	raw, hash := newCredential()
	_, err := s.store.CreateAuthToken(ctx, database.CreateAuthTokenParams{
		UserID:    userID,
		Kind:      kind,
		TokenHash: hash,
		ExpiresAt: time.Now().Add(ttl),
	})
	return raw, err
}

func (s *Service) sendVerification(ctx context.Context, to, credential string) {
	if err := s.mailer.SendVerification(ctx, to, credential); err != nil {
		log.Printf("auth: send verification to %s: %v", to, err)
	}
}

func (s *Service) sendReset(ctx context.Context, to, credential string) {
	if err := s.mailer.SendPasswordReset(ctx, to, credential); err != nil {
		log.Printf("auth: send password reset to %s: %v", to, err)
	}
}

// consume looks up a live credential by its hash.
func (s *Service) consume(ctx context.Context, raw, kind string) (database.AuthToken, error) {
	tok, err := s.store.GetAuthToken(ctx, hashToken(raw), kind)
	if err != nil {
		return database.AuthToken{}, errors.New("not found")
	}
	return tok, nil
}

func newCredential() (raw, hash string) {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	raw = hex.EncodeToString(b)
	return raw, hashToken(raw)
}

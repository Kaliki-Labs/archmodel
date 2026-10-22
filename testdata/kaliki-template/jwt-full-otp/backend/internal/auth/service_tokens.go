package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"fmt"
	"math/big"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	gen "github.com/example/jwt-full-otp-app/backend/gen/api/auth"
	"github.com/example/jwt-full-otp-app/backend/internal/database"
)

const (
	kindVerify        = "verify"
	kindPasswordReset = "password_reset"
)

const (
	verifyTTL = 15 * time.Minute
	resetTTL  = 15 * time.Minute
)

// VerifyEmail implements gen.ServerInterface. Consumes a verification
// code, marks the account verified, and returns a fresh session.
func (s *Service) VerifyEmail(c *gin.Context) {
	var body gen.VerifyEmailJSONRequestBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	subject, err := s.tokens.ParseVerification(body.VerificationToken)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid or expired token"})
		return
	}
	userID, err := uuid.Parse(subject)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid or expired token"})
		return
	}
	tok, err := s.consume(c.Request.Context(), userID, kindVerify, body.Code)
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

// ResendVerification implements gen.ServerInterface. Issues a fresh
// verification credential for an unverified account, same
// issueVerification/sendVerification pattern as Signup's unverified branch,
// and returns the same SignupResponse shape (a new verification_token paired
// with the freshly emailed OTP). Hard-rate-limited (x-rate-limit: 1/60s, no
// burst) at the spec level — this is a resend/email-bombing vector, not just
// a brute-force one.
func (s *Service) ResendVerification(c *gin.Context) {
	var body gen.ResendVerificationJSONRequestBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	user, err := s.store.GetUserByEmail(c.Request.Context(), string(body.Email))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no such account"})
		return
	}
	if user.Verified {
		c.JSON(http.StatusBadRequest, gin.H{"error": "already verified"})
		return
	}

	credential, err := s.issueVerification(c.Request.Context(), user.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not issue verification"})
		return
	}
	s.sendVerification(c.Request.Context(), user.Email, credential)

	verificationToken, err := s.tokens.IssueVerification(user.ID.String())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not issue verification token"})
		return
	}
	c.JSON(http.StatusCreated, gen.SignupResponse{
		User:              toAPIUser(user),
		VerificationToken: verificationToken,
	})
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

	user, err := s.store.GetUserByEmail(c.Request.Context(), string(body.Email))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid or expired code"})
		return
	}
	tok, err := s.consume(c.Request.Context(), user.ID, kindPasswordReset, body.Code)
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

// consume atomically claims an attempt against the latest live credential for
// the user/kind (capped at 5 attempts; see IncrementAuthTokenAttempts), then
// compares it to the supplied code in constant time. No eligible row — expired,
// already used, or attempts exhausted — surfaces identically as "not found".
func (s *Service) consume(ctx context.Context, userID uuid.UUID, kind, code string) (database.AuthToken, error) {
	tok, err := s.store.IncrementAuthTokenAttempts(ctx, userID, kind)
	if err != nil {
		return database.AuthToken{}, errors.New("not found")
	}
	if subtle.ConstantTimeCompare([]byte(tok.TokenHash), []byte(hashToken(code))) != 1 {
		return database.AuthToken{}, errors.New("mismatch")
	}
	return tok, nil
}

func newCredential() (raw, hash string) {
	n, _ := rand.Int(rand.Reader, big.NewInt(1000000))
	raw = fmt.Sprintf("%06d", n.Int64())
	return raw, hashToken(raw)
}

package auth

import (
	"errors"
	"slices"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/example/jwt-full-otp-app/backend/internal/config"
)

// TokenIssuer issues and verifies HS256 JWTs.
type TokenIssuer struct {
	secret []byte
	expiry time.Duration
}

func NewTokenIssuer(cfg config.TokenConfig) *TokenIssuer {
	expiry := time.Duration(cfg.ExpiryHours) * time.Hour
	if expiry == 0 {
		expiry = 24 * time.Hour
	}
	return &TokenIssuer{secret: []byte(cfg.Secret), expiry: expiry}
}

type Claims struct {
	Role string `json:"role"`
	jwt.RegisteredClaims
}

// verifyPurpose marks a token as a pending-verification credential (returned by
// signup, redeemed alongside the OTP). It is NOT a session token: it carries no
// role and cannot authenticate API calls — Parse rejects it (wrong audience).
const verifyPurpose = "verify"

// IssueVerification mints a short-lived token scoping an OTP submission to the
// just-signed-up user. The raw OTP is still emailed separately.
func (t *TokenIssuer) IssueVerification(subject string) (string, error) {
	claims := jwt.RegisteredClaims{
		Subject:   subject,
		Audience:  jwt.ClaimStrings{verifyPurpose},
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(15 * time.Minute)),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(t.secret)
}

// ParseVerification validates a verification token and returns its subject
// (the user id). It rejects ordinary session tokens.
func (t *TokenIssuer) ParseVerification(token string) (string, error) {
	claims := &jwt.RegisteredClaims{}
	parsed, err := jwt.ParseWithClaims(token, claims, func(tok *jwt.Token) (any, error) {
		if _, ok := tok.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return t.secret, nil
	})
	if err != nil || !parsed.Valid {
		return "", errors.New("invalid token")
	}
	if slices.Contains(claims.Audience, verifyPurpose) {
		return claims.Subject, nil
	}
	return "", errors.New("invalid token")
}

func (t *TokenIssuer) Issue(subject, role string) (string, error) {
	claims := Claims{
		Role: role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   subject,
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(t.expiry)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(t.secret)
}

func (t *TokenIssuer) Parse(token string) (*Claims, error) {
	claims := &Claims{}
	parsed, err := jwt.ParseWithClaims(token, claims, func(tok *jwt.Token) (any, error) {
		if _, ok := tok.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return t.secret, nil
	})
	if err != nil || !parsed.Valid {
		return nil, errors.New("invalid token")
	}
	// Reject pending-verification tokens: they are not sessions and must never
	// authenticate API calls (see IssueVerification).
	if slices.Contains(claims.Audience, verifyPurpose) {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}

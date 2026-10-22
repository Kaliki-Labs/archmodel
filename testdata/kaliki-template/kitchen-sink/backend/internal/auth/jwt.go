package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/example/kitchen-sink-app/backend/internal/config"
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
	return claims, nil
}

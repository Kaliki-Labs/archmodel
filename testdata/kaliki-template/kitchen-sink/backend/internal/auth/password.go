package auth

import (
	"errors"
	"strings"
	"unicode"
)

// Password policy. maxPasswordLen caps at 72 bytes because that is the practical
// input limit for the argon2id hashing here (and matches bcrypt-style limits, so
// swapping the hasher later needs no policy change).
const (
	minPasswordLen = 8
	maxPasswordLen = 72
)

// ValidatePassword enforces the account password policy: length 8–72, at least
// one lowercase letter, one uppercase letter, one digit, one symbol, and no
// leading/trailing whitespace. It returns a specific error per failed rule so
// callers can surface an actionable message. Length is measured in bytes to
// match the hasher's input limit.
func ValidatePassword(pw string) error {
	if len(pw) < minPasswordLen {
		return errors.New("password must be at least 8 characters")
	}
	if len(pw) > maxPasswordLen {
		return errors.New("password must be at most 72 characters")
	}
	if strings.TrimSpace(pw) != pw {
		return errors.New("password must not start or end with whitespace")
	}

	var hasLower, hasUpper, hasDigit, hasSymbol bool
	for _, r := range pw {
		switch {
		case unicode.IsLower(r):
			hasLower = true
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsDigit(r):
			hasDigit = true
		case unicode.IsPunct(r) || unicode.IsSymbol(r):
			hasSymbol = true
		}
	}
	switch {
	case !hasLower:
		return errors.New("password must contain a lowercase letter")
	case !hasUpper:
		return errors.New("password must contain an uppercase letter")
	case !hasDigit:
		return errors.New("password must contain a digit")
	case !hasSymbol:
		return errors.New("password must contain a symbol")
	}
	return nil
}

package ratelimit

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"

	"github.com/gin-gonic/gin"
)

// extractKey resolves the bucket-key for rule's scope against the current
// request, returning the scope label actually used (so IP-fallback is visible
// in the bucket namespace) and the key value itself. IP is always resolvable,
// so every other scope falls back to it on resolution failure — "still
// limited" beats "silently unlimited" (see judgment calls in CONTEXT.md / the
// rate-limiting ADR). extractKey never touches the Redis backend, so it's
// testable with a nil-backend Limiter.
func (l *Limiter) extractKey(c *gin.Context, rule Rule) (scopeLabel, key string) {
	switch {
	case rule.Scope == "ip":
		return "ip", clientIP(c)

	case rule.Scope == "identifier":
		if v, ok := identifierFromBody(c, rule.Key); ok {
			return "identifier", v
		}
		return "ip", clientIP(c)

	case rule.Scope == "user":
		if v := c.GetString("user_id"); v != "" {
			return "user", v
		}
		return "ip", clientIP(c)

	case strings.HasPrefix(rule.Scope, "custom:"):
		name := strings.TrimPrefix(rule.Scope, "custom:")
		if extractor, ok := l.custom[name]; ok {
			if v, ok := extractor(c); ok {
				return rule.Scope, v
			}
		}
		return "ip", clientIP(c)

	default:
		// Unrecognized scope (shouldn't happen past ParseRule validation, but
		// fail toward "still limited" rather than panicking mid-request).
		return "ip", clientIP(c)
	}
}

func clientIP(c *gin.Context) string {
	return c.ClientIP()
}

// identifierFromBody peeks the named top-level string field out of a JSON
// request body without consuming it: c.Request.Body is read in full and
// immediately replaced with a fresh reader over the same bytes, so the real
// handler's later c.ShouldBindJSON still sees the complete body. An absent
// field, a non-string value, or malformed JSON are all resolution failures
// (the caller falls back to IP-scoping) — never an error response of their
// own; body validation is the handler's job, not the limiter's.
func identifierFromBody(c *gin.Context, field string) (string, bool) {
	if field == "" || c.Request == nil || c.Request.Body == nil {
		return "", false
	}
	body, err := io.ReadAll(c.Request.Body)
	_ = c.Request.Body.Close()
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	if err != nil {
		return "", false
	}

	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return "", false
	}
	v, ok := m[field].(string)
	if !ok || v == "" {
		return "", false
	}
	return v, true
}

package ratelimit

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis_rate/v10"
	"github.com/redis/go-redis/v9"
)

// limiterBackend is a narrow seam over the one redis_rate.Limiter method this
// package calls, so tests can substitute a fake (e.g. to exercise fail-open on
// a backend error) without standing up a real Redis.
type limiterBackend interface {
	Allow(ctx context.Context, key string, limit redis_rate.Limit) (*redis_rate.Result, error)
}

// KeyExtractor resolves a custom:<name> scope's bucket key from the request.
// Returning ok == false is resolution failure — PerOperation falls back to
// IP-scoping, same as every other scope's failure path.
type KeyExtractor func(c *gin.Context) (key string, ok bool)

// Limiter enforces token-bucket rate limits backed by Redis (GCRA via
// redis_rate). It backs both mechanisms this package implements — PerOperation
// (x-rate-limit-declared, always active) and GlobalFloor (opt-in, IP-scoped
// baseline) — on distinct Redis key namespaces so the two never share a bucket
// even when both apply to the same request.
type Limiter struct {
	backend limiterBackend
	custom  map[string]KeyExtractor
}

// New wires a Limiter against a real Redis client. custom supplies zero or
// more custom:<name> key extractors; the template ships none built-in —
// projects register their own at construction time (see CONTEXT.md glossary).
func New(rdb *redis.Client, custom map[string]KeyExtractor) *Limiter {
	if custom == nil {
		custom = map[string]KeyExtractor{}
	}
	return &Limiter{backend: redis_rate.NewLimiter(rdb), custom: custom}
}

// PerOperation enforces each operation's x-rate-limit rule, if it declared
// one, via lookup — keyed by the operation ID the generated wrapper stashed on
// the context (OperationIDKey). Operations with no rule pass through
// untouched. Per-operation rules always stack with GlobalFloor when the latter
// is enabled; they are never a substitute for it.
func (l *Limiter) PerOperation(lookup RuleLookup) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw, _ := c.Get(OperationIDKey)
		opID, _ := raw.(string)

		rule, ok := lookup(opID)
		if !ok {
			return
		}

		scopeLabel, key := l.extractKey(c, rule)
		// op:<operationID>:<scopeLabel>:<key> — operation ID is part of the
		// bucket key so two different operations sharing a scope+key (e.g. two
		// identifier-scoped endpoints both keyed on the same email) never share
		// a bucket.
		bucketKey := fmt.Sprintf("op:%s:%s:%s", opID, scopeLabel, key)
		l.enforce(c, rule, bucketKey)
	}
}

// GlobalFloor applies a single baseline IP-scoped rule to every request,
// independent of (and stacking with) any PerOperation rule. Call-site gated by
// a runtime config flag (RateLimitConfig.GlobalEnabled) — default off, since
// an undeclared blanket limit is a production surprise.
func (l *Limiter) GlobalFloor(rule Rule) gin.HandlerFunc {
	return func(c *gin.Context) {
		bucketKey := "global:ip:" + clientIP(c)
		l.enforce(c, rule, bucketKey)
	}
}

// enforce runs the token-bucket check for key and either lets the request
// through or aborts it with 429 + Retry-After. A Redis backend error fails
// OPEN (logged, request allowed): a limiter outage must not become a site
// outage.
func (l *Limiter) enforce(c *gin.Context, rule Rule, key string) {
	res, err := l.backend.Allow(c.Request.Context(), "ratelrl:"+key, redis_rate.Limit{
		Rate:   rule.Requests,
		Burst:  rule.Requests + rule.Burst,
		Period: rule.Window,
	})
	if err != nil {
		log.Printf("ratelimit: backend error, failing open: %v", err)
		return
	}
	if res.Allowed > 0 {
		return
	}

	retryAfter := res.RetryAfter
	if retryAfter < 0 {
		retryAfter = rule.Window
	}
	c.Header("Retry-After", fmt.Sprintf("%.0f", retryAfter.Seconds()))
	// Reuses the handwritten gin.H{"error": ...} convention used throughout
	// service.go.jinja/middleware.go.jinja, not oapi-codegen's own {"msg": ...}
	// error-handler shape — don't introduce a second error shape.
	c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "rate limit exceeded"})
}

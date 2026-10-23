// Package ratelimit implements token-bucket rate limiting (Redis-backed via
// GCRA) declared per-operation through the OpenAPI `x-rate-limit` vendor
// extension, plus a separately-gated global per-IP floor. See CONTEXT.md and
// docs/adr/0001-redis-and-rate-limiting-are-unconditional-infra.md for the design.
package ratelimit

import (
	"errors"
	"fmt"
	"time"
)

// Rule is a token-bucket rate-limit declaration for one operation:
//
//	x-rate-limit:
//	  scope: ip | identifier | user | custom:<name>
//	  requests: 10   # steady-state tokens per Window
//	  window: 60s
//	  burst: 5       # extra capacity on top of the steady rate
//	  key: email     # only meaningful (and required) for scope: identifier
type Rule struct {
	Scope    string
	Key      string
	Requests int
	Window   time.Duration
	Burst    int
}

// ParseRule validates and builds a Rule from the raw x-rate-limit extension
// data. Generated code calls this (via MustParseRule) once per rule at
// package-init time, not per-request — a malformed spec is a build/startup
// error, never a runtime cost.
func ParseRule(spec map[string]any) (Rule, error) {
	scope, _ := spec["scope"].(string)
	if scope == "" {
		return Rule{}, errors.New("ratelimit: x-rate-limit.scope is required")
	}

	key, _ := spec["key"].(string)
	if scope == "identifier" && key == "" {
		return Rule{}, errors.New(`ratelimit: x-rate-limit.key is required for scope "identifier"`)
	}

	requests, err := toInt(spec["requests"])
	if err != nil || requests <= 0 {
		return Rule{}, fmt.Errorf("ratelimit: x-rate-limit.requests must be a positive integer, got %v", spec["requests"])
	}

	windowStr, _ := spec["window"].(string)
	window, err := time.ParseDuration(windowStr)
	if err != nil {
		return Rule{}, fmt.Errorf("ratelimit: x-rate-limit.window %q: %w", windowStr, err)
	}
	if window <= 0 {
		return Rule{}, fmt.Errorf("ratelimit: x-rate-limit.window must be positive, got %q", windowStr)
	}

	burst, err := toInt(spec["burst"])
	if err != nil || burst < 0 {
		return Rule{}, fmt.Errorf("ratelimit: x-rate-limit.burst must be a non-negative integer, got %v", spec["burst"])
	}

	return Rule{Scope: scope, Key: key, Requests: requests, Window: window, Burst: burst}, nil
}

// MustParseRule panics on a malformed spec. Generated code (gin-register.tmpl)
// calls this from a package-level var initializer, so a bad x-rate-limit
// extension fails loudly at build/startup rather than misbehaving per-request.
func MustParseRule(spec map[string]any) Rule {
	r, err := ParseRule(spec)
	if err != nil {
		panic(err)
	}
	return r
}

// toInt accepts the numeric shapes a map[string]any might hold after being
// deserialized from an OpenAPI spec (float64 from encoding/json, plain int
// from a hand-built test fixture).
func toInt(v any) (int, error) {
	switch n := v.(type) {
	case int:
		return n, nil
	case int64:
		return int(n), nil
	case float64:
		return int(n), nil
	default:
		return 0, fmt.Errorf("ratelimit: expected a number, got %T", v)
	}
}

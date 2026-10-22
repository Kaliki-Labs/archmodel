package ratelimit

// OperationIDKey is the gin context key each generated wrapper stashes its
// operation ID under (gin-wrappers.tmpl override — mirrors how oapi-codegen
// already stashes BearerAuthScopes per-operation for auth), so PerOperation
// can look up which Rule, if any, applies to the current request.
const OperationIDKey = "ratelimit.operationID"

// RuleLookup resolves an operation ID to its Rule, if that operation declared
// one via x-rate-limit. Each generated package exposes its own RateLimitRule
// func matching this signature (gin-register.tmpl override); operations with
// no x-rate-limit extension are simply absent from the generated map.
type RuleLookup func(operationID string) (Rule, bool)

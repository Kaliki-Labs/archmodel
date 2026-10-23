package auth

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	gen "github.com/example/kitchen-sink-app/backend/gen/api/auth"
)

// authenticate validates the Bearer token. On success it stores user_id/role on
// the context and returns the claims; on failure it writes a 401 and returns
// false. It backs ScopeAuth and is also usable directly from a handler.
func (s *Service) authenticate(c *gin.Context) (*Claims, bool) {
	const prefix = "Bearer "
	header := c.GetHeader("Authorization")
	if !strings.HasPrefix(header, prefix) {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing bearer token"})
		return nil, false
	}
	claims, err := s.tokens.Parse(strings.TrimPrefix(header, prefix))
	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
		return nil, false
	}
	c.Set("user_id", claims.Subject)
	c.Set("role", claims.Role)
	return claims, true
}

// ScopeAuth is a per-operation middleware that makes the OpenAPI spec the single
// source of truth for which endpoints require authentication. oapi-codegen sets
// the BearerAuthScopes key on the context inside each generated wrapper for
// operations that declare `security: - bearerAuth: []`, and runs option-supplied
// middlewares right after that Set (aborting the chain if one aborts). So: when
// the key is present the request must carry a valid bearer token; otherwise the
// endpoint is public and passes through. Register it via RegisterHandlersWithOptions.
//
// The same key string ("bearerAuth.Scopes") is emitted by every gen package that
// names its scheme "bearerAuth", so this one middleware also guards other domains
// (e.g. items) once they mark endpoints in their spec — no per-endpoint wiring.
func (s *Service) ScopeAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, required := c.Get(gen.BearerAuthScopes); !required {
			return // endpoint declares no security requirement — public
		}
		// authenticate writes 401 + aborts on failure, which stops the wrapper.
		s.authenticate(c)
	}
}

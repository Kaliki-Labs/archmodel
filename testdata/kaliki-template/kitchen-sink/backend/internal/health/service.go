// Package health implements the always-present liveness probe. Like the items
// example, it demonstrates the full vertical slice: OpenAPI spec ->
// oapi-codegen (gin-server interface) -> handler. Unlike items it has no
// store/db dependency and is always registered, regardless of auth or
// include_example_domain, so a freshly generated project always shows the
// codegen pipeline end to end.
package health

import (
	"net/http"

	"github.com/gin-gonic/gin"

	gen "github.com/example/kitchen-sink-app/backend/gen/api/health"
)

// Service implements gen.ServerInterface (generated from api/services/health.yaml).
type Service struct{}

func New() *Service { return &Service{} }

// Register mounts the generated routes directly on r. Health has no auth
// middleware — it's always public.
func (s *Service) Register(r gin.IRouter) {
	gen.RegisterHandlers(r, s)
}

// GetHealth implements gen.ServerInterface.
func (s *Service) GetHealth(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

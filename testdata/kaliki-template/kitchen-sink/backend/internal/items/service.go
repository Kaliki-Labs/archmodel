// Package items is the disposable EXAMPLE domain. It demonstrates the full
// vertical slice: OpenAPI spec -> oapi-codegen (gin-server interface) -> handler
// -> sqlc-generated store -> postgres. Copy this pattern for real domains, then
// delete this package (see TEMPLATE_NOTES.md).
package items

import (
	"net/http"

	"github.com/gin-gonic/gin"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/jackc/pgx/v5/pgxpool"

	gen "github.com/example/kitchen-sink-app/backend/gen/api/items"
	"github.com/example/kitchen-sink-app/backend/internal/database"
)

// Service implements gen.ServerInterface (generated from api/services/items.yaml).
type Service struct {
	store Store
}

func New(pool *pgxpool.Pool) *Service { return &Service{store: NewStore(pool)} }

// Register mounts the generated routes under the given router group. Pass the
// auth scope middleware (auth.Service.ScopeAuth) so that any items endpoint the
// OpenAPI spec marks with `security: bearerAuth` is enforced; endpoints without
// it stay public. With no middleware every endpoint is public (auth == none).
func (s *Service) Register(r gin.IRouter, authMW ...gin.HandlerFunc) {
	mws := make([]gen.MiddlewareFunc, len(authMW))
	for i, m := range authMW {
		mws[i] = gen.MiddlewareFunc(m)
	}
	gen.RegisterHandlersWithOptions(r, s, gen.GinServerOptions{Middlewares: mws})
}

// ListItems implements gen.ServerInterface.
func (s *Service) ListItems(c *gin.Context) {
	items, err := s.store.ListItems(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	out := make([]gen.Item, len(items))
	for i, it := range items {
		out[i] = toAPIItem(it)
	}
	c.JSON(http.StatusOK, gen.ItemList{Items: out})
}

// CreateItem implements gen.ServerInterface.
func (s *Service) CreateItem(c *gin.Context) {
	var body gen.CreateItemJSONRequestBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if body.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}
	item, err := s.store.CreateItem(c.Request.Context(), body.Name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, toAPIItem(item))
}

// GetItem implements gen.ServerInterface.
func (s *Service) GetItem(c *gin.Context, id openapi_types.UUID) {
	item, err := s.store.GetItem(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, toAPIItem(item))
}

// toAPIItem maps the sqlc row to the generated API type. Responses go through
// the gen types (not raw database structs) so the JSON contract is owned by the
// OpenAPI spec — that's why the database structs carry no json tags.
func toAPIItem(i database.Item) gen.Item {
	return gen.Item{Id: i.ID, Name: i.Name, CreatedAt: i.CreatedAt}
}

package items

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/example/kitchen-sink-app/backend/internal/database"
)

// Store is the persistence seam for the items domain: a hand-written interface
// narrowed to the queries this domain uses. It exists so the service depends on
// behaviour, not on the generated struct — the test seam (pass a fake) and the
// future transaction seam. The sqlc-generated *database.Queries satisfies it
// directly, so NewStore just returns that; database.Queries.WithTx also returns
// *Queries, so a transactional store satisfies the same interface for free.
//
// Method names mirror the generated queries — "go to implementation" lands on
// the sqlc method (its SQL const sits right above it). The breadcrumb names the
// source file so the mapping is greppable without an IDE.
type Store interface {
	// ListItems — sql/queries/items.sql
	ListItems(ctx context.Context) ([]database.Item, error)
	// CreateItem — sql/queries/items.sql
	CreateItem(ctx context.Context, name string) (database.Item, error)
	// GetItem — sql/queries/items.sql
	GetItem(ctx context.Context, id uuid.UUID) (database.Item, error)
}

// NewStore returns a Postgres-backed Store over the shared pool.
func NewStore(pool *pgxpool.Pool) Store { return database.New(pool) }

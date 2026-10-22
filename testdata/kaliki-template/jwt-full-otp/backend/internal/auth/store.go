package auth

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/example/jwt-full-otp-app/backend/internal/database"
)

// Store is the persistence seam for the auth domain: a hand-written interface
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
	// CreateUser — sql/queries/auth.sql
	CreateUser(ctx context.Context, arg database.CreateUserParams) (database.User, error)
	// GetUserByEmail — sql/queries/auth.sql
	GetUserByEmail(ctx context.Context, email string) (database.User, error)
	// GetUserByID — sql/queries/auth.sql
	GetUserByID(ctx context.Context, id uuid.UUID) (database.User, error)
	// CreateRefreshToken — sql/queries/auth.sql
	CreateRefreshToken(ctx context.Context, arg database.CreateRefreshTokenParams) (database.RefreshToken, error)
	// GetRefreshToken — sql/queries/auth.sql
	GetRefreshToken(ctx context.Context, tokenHash string) (database.RefreshToken, error)
	// RevokeRefreshToken — sql/queries/auth.sql
	RevokeRefreshToken(ctx context.Context, id uuid.UUID) error
	// VerifyUser — sql/queries/auth.sql
	VerifyUser(ctx context.Context, id uuid.UUID) error
	// UpdateUserPassword — sql/queries/auth.sql (2 params -> positional)
	UpdateUserPassword(ctx context.Context, id uuid.UUID, passwordHash string) error
	// CreateAuthToken — sql/queries/auth.sql
	CreateAuthToken(ctx context.Context, arg database.CreateAuthTokenParams) (database.AuthToken, error)
	// IncrementAuthTokenAttempts — sql/queries/auth.sql (2 params -> positional)
	IncrementAuthTokenAttempts(ctx context.Context, userID uuid.UUID, kind string) (database.AuthToken, error)
	// MarkAuthTokenUsed — sql/queries/auth.sql
	MarkAuthTokenUsed(ctx context.Context, id uuid.UUID) error
}

// Compile-time assertion that the generated Queries satisfies Store, so a query
// signature drift (e.g. a changed query_parameter_limit) fails here with a clear
// message rather than at each call site.
var _ Store = (*database.Queries)(nil)

// NewStore returns a Postgres-backed Store over the shared pool.
func NewStore(pool *pgxpool.Pool) Store { return database.New(pool) }

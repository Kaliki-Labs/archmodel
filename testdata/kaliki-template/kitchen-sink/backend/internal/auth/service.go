// Package auth is a kept (non-example) module: concrete users schema, JWT
// signup/login, email verification and password reset, backed by sqlc queries.
package auth

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	openapi_types "github.com/oapi-codegen/runtime/types"

	gen "github.com/example/kitchen-sink-app/backend/gen/api/auth"
	"github.com/example/kitchen-sink-app/backend/internal/config"
	"github.com/example/kitchen-sink-app/backend/internal/database"
	"github.com/example/kitchen-sink-app/backend/internal/mail"
	"github.com/example/kitchen-sink-app/backend/internal/ratelimit"
)

type Service struct {
	store      Store
	tokens     *TokenIssuer
	refreshTTL time.Duration
	mailer     mail.Mailer
	rl         *ratelimit.Limiter
}

func refreshTTL(cfg config.TokenConfig) time.Duration {
	h := cfg.RefreshExpiryHours
	if h == 0 {
		h = 720 // 30 days
	}
	return time.Duration(h) * time.Hour
}

func New(pool *pgxpool.Pool, cfg config.TokenConfig, mailer mail.Mailer, rl *ratelimit.Limiter) *Service {
	return &Service{
		store:      NewStore(pool),
		tokens:     NewTokenIssuer(cfg),
		refreshTTL: refreshTTL(cfg),
		mailer:     mailer,
		rl:         rl,
	}
}

// Register mounts the generated routes under the given router group, guarding
// them with ScopeAuth (OpenAPI `security` blocks decide what needs a token)
// and PerOperation (OpenAPI `x-rate-limit` extensions decide what's throttled).
func (s *Service) Register(r gin.IRouter) {
	gen.RegisterHandlersWithOptions(r, s, gen.GinServerOptions{
		Middlewares: []gen.MiddlewareFunc{
			gen.MiddlewareFunc(s.ScopeAuth()),
			gen.MiddlewareFunc(s.rl.PerOperation(gen.RateLimitRule)),
		},
	})
}

// Signup implements gen.ServerInterface.
func (s *Service) Signup(c *gin.Context) {
	var body gen.SignupJSONRequestBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if body.Email == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "email is required"})
		return
	}
	if err := ValidatePassword(body.Password); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	hash, err := HashPassword(body.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not hash password"})
		return
	}

	name := ""
	if body.Name != nil {
		name = *body.Name
	}

	user, err := s.store.CreateUser(c.Request.Context(), database.CreateUserParams{
		Email:        string(body.Email),
		PasswordHash: hash,
		Name:         name,
		Role:         "user",
	})
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "email already registered"})
		return
	}

	// Account starts unverified. Issue a verification credential and email it.
	// The credential is NEVER returned in the response.
	credential, err := s.issueVerification(c.Request.Context(), user.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not issue verification"})
		return
	}
	s.sendVerification(c.Request.Context(), user.Email, credential)

	// No session until the emailed link is clicked (POST /auth/verify), same
	// as OTP mode; Login/RefreshToken reject unverified accounts too (#14, #30).
	c.JSON(http.StatusCreated, gen.SignupResponse{User: toAPIUser(user)})
}

// Login implements gen.ServerInterface.
func (s *Service) Login(c *gin.Context) {
	var body gen.LoginJSONRequestBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if body.Email == "" || body.Password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "email and password are required"})
		return
	}

	user, err := s.store.GetUserByEmail(c.Request.Context(), string(body.Email))
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}
	if !CheckPassword(user.PasswordHash, body.Password) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}
	if !user.Verified {
		// #14: unverified users never get a session. Issue a fresh verification
		// credential (the old one may be expired/used) and email it, same as
		// Signup's unverified branch.
		credential, err := s.issueVerification(c.Request.Context(), user.ID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "could not issue verification"})
			return
		}
		s.sendVerification(c.Request.Context(), user.Email, credential)
		c.JSON(http.StatusForbidden, gin.H{"error": "email_not_verified"})
		return
	}

	s.respondWithSession(c, http.StatusOK, user)
}

func toAPIUser(u database.User) gen.User {
	created := u.CreatedAt
	return gen.User{
		Id:        u.ID,
		Email:     openapi_types.Email(u.Email),
		Name:      u.Name,
		Role:      u.Role,
		CreatedAt: &created,
		Verified:  &u.Verified,
	}
}

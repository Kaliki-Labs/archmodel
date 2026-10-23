package auth_test

import (
	"context"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"github.com/example/kitchen-sink-app/backend/internal/auth"
	"github.com/example/kitchen-sink-app/backend/internal/config"
	"github.com/example/kitchen-sink-app/backend/internal/ratelimit"
	"github.com/example/kitchen-sink-app/backend/internal/testsupport"
)

var (
	testDB      *testsupport.TestDB
	router      *gin.Engine
	redisClient *redis.Client
	mailer *recordingMailer
)

// recordingMailer captures sent credentials so tests can read the value that
// would have been emailed. Production never returns credentials in responses,
// so this is the supported way to exercise the verify/reset flows.
type recordingMailer struct {
	verifications map[string]string
	resets        map[string]string
}

func newRecordingMailer() *recordingMailer {
	return &recordingMailer{verifications: map[string]string{}, resets: map[string]string{}}
}

func (m *recordingMailer) SendVerification(_ context.Context, to, credential string) error {
	m.verifications[to] = credential
	return nil
}

func (m *recordingMailer) SendPasswordReset(_ context.Context, to, credential string) error {
	m.resets[to] = credential
	return nil
}

func TestMain(m *testing.M) {
	testDB = testsupport.Connect("test_auth")

	// Real Redis (docker/docker-compose-test.yaml's redis service), not
	// miniredis: this package's tests are the end-to-end proof that rate
	// limiting works through a real gin.Engine + ratelimit.Limiter stack
	// (internal/ratelimit's own tests cover the limiter logic in isolation
	// against miniredis).
	redisOpts, err := redis.ParseURL(testsupport.RedisURL())
	if err != nil {
		panic("parse test redis url: " + err.Error())
	}
	redisClient = redis.NewClient(redisOpts)
	rl := ratelimit.New(redisClient, nil)

	r, api := testsupport.NewRouter()
	mailer = newRecordingMailer()
	auth.New(testDB.Pool, config.TokenConfig{Secret: "test-secret", ExpiryHours: 1}, mailer, rl).Register(api)
	router = r

	os.Exit(m.Run())
}

func setupTest(t *testing.T) {
	t.Helper()
	testDB.Truncate(t)
	// Rate limit buckets are real Redis state, not per-test DB rows truncated
	// above — without this, two local `go test` runs in a row against the
	// same long-lived docker-compose-test Redis would see the second run
	// start with partially-consumed buckets (GCRA refills slowly), making
	// the rate-limit tests flaky outside a fresh CI container.
	if err := redisClient.FlushAll(context.Background()).Err(); err != nil {
		t.Fatalf("flush test redis: %v", err)
	}
	mailer.verifications = map[string]string{}
	mailer.resets = map[string]string{}
}

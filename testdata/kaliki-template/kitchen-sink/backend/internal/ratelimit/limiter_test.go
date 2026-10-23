package ratelimit

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis_rate/v10"
	"github.com/redis/go-redis/v9"
)

// newTestLimiter wires a Limiter against a real redis_rate.Limiter backed by
// miniredis (an in-memory fake Redis server), so these tests exercise the real
// GCRA algorithm without a real Redis dependency.
func newTestLimiter(t *testing.T) (*Limiter, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return New(rdb, nil), mr
}

func doRequest(l *Limiter, h gin.HandlerFunc, remoteIP string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c.Request.RemoteAddr = remoteIP + ":12345"
	h(c)
	return w
}

func TestLimiterGlobalFloor(t *testing.T) {
	t.Run("allows up to burst, denies the next request", func(t *testing.T) {
		l, _ := newTestLimiter(t)
		rule := Rule{Scope: "ip", Requests: 2, Window: time.Minute, Burst: 0}
		h := l.GlobalFloor(rule)

		for i := 0; i < 2; i++ {
			w := doRequest(l, h, "198.51.100.1")
			if w.Code != http.StatusOK {
				t.Fatalf("request %d: status = %d, want 200 (within burst)", i, w.Code)
			}
		}

		w := doRequest(l, h, "198.51.100.1")
		if w.Code != http.StatusTooManyRequests {
			t.Fatalf("status = %d, want 429 once burst is exhausted", w.Code)
		}
		if w.Header().Get("Retry-After") == "" {
			t.Fatal("expected a Retry-After header on a 429 response")
		}
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["error"] == nil {
			t.Fatalf(`expected {"error": ...} body shape, got %v`, body)
		}
	})

	t.Run("different IPs get independent buckets", func(t *testing.T) {
		l, _ := newTestLimiter(t)
		rule := Rule{Scope: "ip", Requests: 1, Window: time.Minute, Burst: 0}
		h := l.GlobalFloor(rule)

		if w := doRequest(l, h, "198.51.100.1"); w.Code != http.StatusOK {
			t.Fatalf("first IP: status = %d, want 200", w.Code)
		}
		if w := doRequest(l, h, "198.51.100.1"); w.Code != http.StatusTooManyRequests {
			t.Fatalf("first IP, 2nd request: status = %d, want 429", w.Code)
		}
		if w := doRequest(l, h, "198.51.100.2"); w.Code != http.StatusOK {
			t.Fatalf("second IP: status = %d, want 200 (independent bucket)", w.Code)
		}
	})

	t.Run("refills after the window elapses", func(t *testing.T) {
		l, mr := newTestLimiter(t)
		rule := Rule{Scope: "ip", Requests: 1, Window: time.Minute, Burst: 0}
		h := l.GlobalFloor(rule)

		if w := doRequest(l, h, "198.51.100.1"); w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		if w := doRequest(l, h, "198.51.100.1"); w.Code != http.StatusTooManyRequests {
			t.Fatalf("status = %d, want 429 before refill", w.Code)
		}

		mr.FastForward(time.Minute)

		if w := doRequest(l, h, "198.51.100.1"); w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 after the window refills", w.Code)
		}
	})
}

// fakeBackend simulates a Redis/redis_rate failure so enforce's fail-open path
// can be exercised without miniredis.
type fakeBackend struct{ err error }

func (f fakeBackend) Allow(ctx context.Context, key string, limit redis_rate.Limit) (*redis_rate.Result, error) {
	return nil, f.err
}

func TestLimiterFailsOpenOnBackendError(t *testing.T) {
	l := &Limiter{backend: fakeBackend{err: errors.New("redis: connection refused")}}
	rule := Rule{Scope: "ip", Requests: 1, Window: time.Minute, Burst: 0}

	// A healthy limiter would deny the 2nd+ request; a down backend must still
	// allow every request (fail open) rather than turning a limiter outage into
	// a site outage.
	for i := 0; i < 5; i++ {
		w := doRequest(l, l.GlobalFloor(rule), "198.51.100.1")
		if w.Code != http.StatusOK {
			t.Fatalf("request %d: status = %d, want 200 (fail open on backend error)", i, w.Code)
		}
	}
}

func TestLimiterPerOperation(t *testing.T) {
	t.Run("no rule for operation passes through", func(t *testing.T) {
		l, _ := newTestLimiter(t)
		lookup := func(string) (Rule, bool) { return Rule{}, false }
		w := doRequest(l, l.PerOperation(lookup), "198.51.100.1")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (no rule declared)", w.Code)
		}
	})

	t.Run("rule for operation is enforced", func(t *testing.T) {
		l, _ := newTestLimiter(t)
		rule := Rule{Scope: "ip", Requests: 1, Window: time.Minute, Burst: 0}
		lookup := func(opID string) (Rule, bool) {
			if opID == "login" {
				return rule, true
			}
			return Rule{}, false
		}
		h := func(c *gin.Context) {
			c.Set(OperationIDKey, "login")
			l.PerOperation(lookup)(c)
		}

		if w := doRequest(l, h, "198.51.100.1"); w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		if w := doRequest(l, h, "198.51.100.1"); w.Code != http.StatusTooManyRequests {
			t.Fatalf("status = %d, want 429", w.Code)
		}
	})
}

package ratelimit

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// testContext builds a gin.Context over a request with the given JSON body (or
// no body if empty), for extractKey to inspect. extractKey never touches
// Redis, so every test here constructs a Limiter with a nil backend.
func testContext(t *testing.T, body string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	r := httptest.NewRequest("POST", "/", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.RemoteAddr = "203.0.113.7:12345"
	c.Request = r
	return c
}

func TestExtractKey(t *testing.T) {
	t.Run("ip scope", func(t *testing.T) {
		l := &Limiter{}
		c := testContext(t, "")
		scopeLabel, key := l.extractKey(c, Rule{Scope: "ip"})
		if scopeLabel != "ip" {
			t.Fatalf("scopeLabel = %q, want ip", scopeLabel)
		}
		if key != "203.0.113.7" {
			t.Fatalf("key = %q, want 203.0.113.7", key)
		}
	})

	t.Run("identifier scope resolves from body", func(t *testing.T) {
		l := &Limiter{}
		c := testContext(t, `{"email":"a@b.com","password":"x"}`)
		scopeLabel, key := l.extractKey(c, Rule{Scope: "identifier", Key: "email"})
		if scopeLabel != "identifier" || key != "a@b.com" {
			t.Fatalf("got (%q, %q), want (identifier, a@b.com)", scopeLabel, key)
		}
	})

	t.Run("identifier scope falls back to IP when field absent", func(t *testing.T) {
		l := &Limiter{}
		c := testContext(t, `{"password":"x"}`)
		scopeLabel, key := l.extractKey(c, Rule{Scope: "identifier", Key: "email"})
		if scopeLabel != "ip" || key != "203.0.113.7" {
			t.Fatalf("got (%q, %q), want (ip, 203.0.113.7)", scopeLabel, key)
		}
	})

	t.Run("identifier scope falls back to IP on malformed body", func(t *testing.T) {
		l := &Limiter{}
		c := testContext(t, `not json`)
		scopeLabel, key := l.extractKey(c, Rule{Scope: "identifier", Key: "email"})
		if scopeLabel != "ip" || key != "203.0.113.7" {
			t.Fatalf("got (%q, %q), want (ip, 203.0.113.7)", scopeLabel, key)
		}
	})

	t.Run("identifier scope body is restored for the real handler", func(t *testing.T) {
		l := &Limiter{}
		c := testContext(t, `{"email":"a@b.com"}`)
		l.extractKey(c, Rule{Scope: "identifier", Key: "email"})
		var body struct {
			Email string `json:"email"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			t.Fatalf("body not restored: %v", err)
		}
		if body.Email != "a@b.com" {
			t.Fatalf("body.Email = %q, want a@b.com", body.Email)
		}
	})

	t.Run("user scope resolves from context", func(t *testing.T) {
		l := &Limiter{}
		c := testContext(t, "")
		c.Set("user_id", "user-123")
		scopeLabel, key := l.extractKey(c, Rule{Scope: "user"})
		if scopeLabel != "user" || key != "user-123" {
			t.Fatalf("got (%q, %q), want (user, user-123)", scopeLabel, key)
		}
	})

	t.Run("user scope falls back to IP when unauthenticated", func(t *testing.T) {
		l := &Limiter{}
		c := testContext(t, "")
		scopeLabel, key := l.extractKey(c, Rule{Scope: "user"})
		if scopeLabel != "ip" || key != "203.0.113.7" {
			t.Fatalf("got (%q, %q), want (ip, 203.0.113.7)", scopeLabel, key)
		}
	})

	t.Run("custom scope resolves via registered extractor", func(t *testing.T) {
		l := &Limiter{custom: map[string]KeyExtractor{
			"workspace": func(c *gin.Context) (string, bool) { return "ws-42", true },
		}}
		c := testContext(t, "")
		scopeLabel, key := l.extractKey(c, Rule{Scope: "custom:workspace"})
		if scopeLabel != "custom:workspace" || key != "ws-42" {
			t.Fatalf("got (%q, %q), want (custom:workspace, ws-42)", scopeLabel, key)
		}
	})

	t.Run("custom scope falls back to IP when extractor reports not-ok", func(t *testing.T) {
		l := &Limiter{custom: map[string]KeyExtractor{
			"workspace": func(c *gin.Context) (string, bool) { return "", false },
		}}
		c := testContext(t, "")
		scopeLabel, key := l.extractKey(c, Rule{Scope: "custom:workspace"})
		if scopeLabel != "ip" || key != "203.0.113.7" {
			t.Fatalf("got (%q, %q), want (ip, 203.0.113.7)", scopeLabel, key)
		}
	})

	t.Run("custom scope falls back to IP when unregistered", func(t *testing.T) {
		l := &Limiter{custom: map[string]KeyExtractor{}}
		c := testContext(t, "")
		scopeLabel, key := l.extractKey(c, Rule{Scope: "custom:unknown"})
		if scopeLabel != "ip" || key != "203.0.113.7" {
			t.Fatalf("got (%q, %q), want (ip, 203.0.113.7)", scopeLabel, key)
		}
	})
}

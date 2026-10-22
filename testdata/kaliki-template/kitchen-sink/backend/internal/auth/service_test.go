package auth_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/example/kitchen-sink-app/backend/internal/testsupport"
)

func decode(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("decode: %v (%s)", err, string(b))
	}
	return m
}

func TestSignup(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		setupTest(t)

		w := testsupport.DoJSON(router, http.MethodPost, "/api/v1/auth/signup",
			`{"email":"a@b.com","password":"Password123!","name":"A"}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201 (%s)", w.Code, w.Body.String())
		}
		body := decode(t, w.Body.Bytes())
		// Signup does not start a session. No access/refresh tokens until the
		// email is verified.
		if body["token"] != nil || body["refresh_token"] != nil {
			t.Fatal("signup must not issue a session before verification")
		}
		if body["user"] == nil {
			t.Fatal("expected the created user in the response")
		}

		// Password must be stored hashed, never in plaintext.
		var hash string
		if err := testDB.Pool.QueryRow(context.Background(),
			`SELECT password_hash FROM users WHERE email = $1`, "a@b.com").Scan(&hash); err != nil {
			t.Fatalf("query: %v", err)
		}
		if hash == "" || hash == "Password123!" {
			t.Fatalf("password not hashed: %q", hash)
		}
		// The verification credential is emailed, never returned in the response.
		if body["code"] != nil || body["verify_token"] != nil {
			t.Fatal("credential must not be returned in the response")
		}
		if mailer.verifications["a@b.com"] == "" {
			t.Fatal("expected a verification credential to be emailed")
		}
	})

	t.Run("conflict", func(t *testing.T) {
		setupTest(t)

		testsupport.DoJSON(router, http.MethodPost, "/api/v1/auth/signup",
			`{"email":"dup@b.com","password":"Password123!"}`)
		w := testsupport.DoJSON(router, http.MethodPost, "/api/v1/auth/signup",
			`{"email":"dup@b.com","password":"Password123!"}`)
		if w.Code != http.StatusConflict {
			t.Fatalf("status = %d, want 409", w.Code)
		}
	})

	t.Run("validation", func(t *testing.T) {
		setupTest(t)

		w := testsupport.DoJSON(router, http.MethodPost, "/api/v1/auth/signup",
			`{"email":"a@b.com","password":"short"}`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", w.Code)
		}
	})
}

func TestLogin(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		setupTest(t)
		// signupSession verifies the account (#14: Login now rejects
		// unverified users), which also conveniently sets the password.
		signupSession(t, "u@b.com")

		w := testsupport.DoJSON(router, http.MethodPost, "/api/v1/auth/login",
			`{"email":"u@b.com","password":"Password123!"}`)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (%s)", w.Code, w.Body.String())
		}
		if decode(t, w.Body.Bytes())["token"] == nil {
			t.Fatal("expected a token")
		}
	})

	t.Run("authorization", func(t *testing.T) {
		setupTest(t)
		signupSession(t, "u@b.com")

		w := testsupport.DoJSON(router, http.MethodPost, "/api/v1/auth/login",
			`{"email":"u@b.com","password":"wrongpass"}`)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", w.Code)
		}
	})

	t.Run("validation", func(t *testing.T) {
		setupTest(t)

		w := testsupport.DoJSON(router, http.MethodPost, "/api/v1/auth/login", `{}`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", w.Code)
		}
	})
}

// signupSession signs up and returns the (access, refresh) pair.
func signupSession(t *testing.T, email string) (string, string) {
	t.Helper()
	w := testsupport.DoJSON(router, http.MethodPost, "/api/v1/auth/signup",
		`{"email":"`+email+`","password":"Password123!"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("signup status = %d (%s)", w.Code, w.Body.String())
	}
	b := decode(t, w.Body.Bytes())
	// Signup returns no session in token mode either (#30); verify via the
	// emailed token to obtain one.
	credential := mailer.verifications[email]
	if credential == "" {
		t.Fatalf("no verification credential captured for %s", email)
	}
	vw := testsupport.DoJSON(router, http.MethodPost, "/api/v1/auth/verify",
		`{"token":"`+credential+`"}`)
	if vw.Code != http.StatusOK {
		t.Fatalf("verify status = %d (%s)", vw.Code, vw.Body.String())
	}
	b = decode(t, vw.Body.Bytes())
	return b["token"].(string), b["refresh_token"].(string)
}

func TestGetCurrentUser(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		setupTest(t)
		access, _ := signupSession(t, "me@b.com")

		w := testsupport.DoJSONAuth(router, http.MethodGet, "/api/v1/auth/me", "", access)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (%s)", w.Code, w.Body.String())
		}
		if decode(t, w.Body.Bytes())["email"] != "me@b.com" {
			t.Fatalf("unexpected user: %s", w.Body.String())
		}
	})

	t.Run("authorization", func(t *testing.T) {
		setupTest(t)

		w := testsupport.DoJSON(router, http.MethodGet, "/api/v1/auth/me", "")
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", w.Code)
		}
	})
}

func TestRefreshToken(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		setupTest(t)
		_, refresh := signupSession(t, "rt@b.com")

		w := testsupport.DoJSON(router, http.MethodPost, "/api/v1/auth/refresh",
			`{"refresh_token":"`+refresh+`"}`)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (%s)", w.Code, w.Body.String())
		}
		if decode(t, w.Body.Bytes())["token"] == nil {
			t.Fatal("expected a new access token")
		}

		// Rotation: the old refresh token is revoked and cannot be reused.
		reuse := testsupport.DoJSON(router, http.MethodPost, "/api/v1/auth/refresh",
			`{"refresh_token":"`+refresh+`"}`)
		if reuse.Code != http.StatusUnauthorized {
			t.Fatalf("reused refresh status = %d, want 401", reuse.Code)
		}
	})

	t.Run("authorization", func(t *testing.T) {
		setupTest(t)

		w := testsupport.DoJSON(router, http.MethodPost, "/api/v1/auth/refresh",
			`{"refresh_token":"nope"}`)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", w.Code)
		}
	})
}

func TestLogout(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		setupTest(t)
		_, refresh := signupSession(t, "lo@b.com")

		w := testsupport.DoJSON(router, http.MethodPost, "/api/v1/auth/logout",
			`{"refresh_token":"`+refresh+`"}`)
		if w.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want 204 (%s)", w.Code, w.Body.String())
		}

		// The refresh token is revoked after logout.
		after := testsupport.DoJSON(router, http.MethodPost, "/api/v1/auth/refresh",
			`{"refresh_token":"`+refresh+`"}`)
		if after.Code != http.StatusUnauthorized {
			t.Fatalf("refresh after logout = %d, want 401", after.Code)
		}
	})
}

func TestVerifyEmail(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		setupTest(t)

		signupResp := testsupport.DoJSON(router, http.MethodPost, "/api/v1/auth/signup",
			`{"email":"v@b.com","password":"Password123!"}`)
		credential := mailer.verifications["v@b.com"]
		if credential == "" {
			t.Fatal("no verification credential captured")
		}
		_ = signupResp
		w := testsupport.DoJSON(router, http.MethodPost, "/api/v1/auth/verify",
			`{"token":"`+credential+`"}`)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (%s)", w.Code, w.Body.String())
		}

		var verified bool
		if err := testDB.Pool.QueryRow(context.Background(),
			`SELECT verified FROM users WHERE email = $1`, "v@b.com").Scan(&verified); err != nil {
			t.Fatalf("query: %v", err)
		}
		if !verified {
			t.Fatal("user should be verified")
		}
	})

	t.Run("validation", func(t *testing.T) {
		setupTest(t)
		w := testsupport.DoJSON(router, http.MethodPost, "/api/v1/auth/verify",
			`{"token":"not-a-real-token"}`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", w.Code)
		}
	})
}

func TestConfirmPasswordReset(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		setupTest(t)

		signupResp := testsupport.DoJSON(router, http.MethodPost, "/api/v1/auth/signup",
			`{"email":"r@b.com","password":"Password123!"}`)
		// Login's new #14 verified-check (below) requires a verified account.
		_ = signupResp
		vw := testsupport.DoJSON(router, http.MethodPost, "/api/v1/auth/verify",
			`{"token":"`+mailer.verifications["r@b.com"]+`"}`)
		if vw.Code != http.StatusOK {
			t.Fatalf("verify status = %d, want 200 (%s)", vw.Code, vw.Body.String())
		}

		req := testsupport.DoJSON(router, http.MethodPost, "/api/v1/auth/password-reset/request",
			`{"email":"r@b.com"}`)
		if req.Code != http.StatusOK {
			t.Fatalf("request status = %d, want 200", req.Code)
		}
		credential := mailer.resets["r@b.com"]
		if credential == "" {
			t.Fatal("no reset credential captured")
		}
		w := testsupport.DoJSON(router, http.MethodPost, "/api/v1/auth/password-reset/confirm",
			`{"token":"`+credential+`","password":"NewPassword123!"}`)
		if w.Code != http.StatusOK {
			t.Fatalf("confirm status = %d, want 200 (%s)", w.Code, w.Body.String())
		}

		// New password works.
		login := testsupport.DoJSON(router, http.MethodPost, "/api/v1/auth/login",
			`{"email":"r@b.com","password":"NewPassword123!"}`)
		if login.Code != http.StatusOK {
			t.Fatalf("login with new password = %d, want 200", login.Code)
		}
	})

	t.Run("validation", func(t *testing.T) {
		setupTest(t)
		w := testsupport.DoJSON(router, http.MethodPost, "/api/v1/auth/password-reset/confirm",
			`{"token":"bad","password":"NewPassword123!"}`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", w.Code)
		}
	})
}

// #14: unverified users never get a session from Login or RefreshToken.

func TestLoginUnverifiedUser(t *testing.T) {
	setupTest(t)
	testsupport.DoJSON(router, http.MethodPost, "/api/v1/auth/signup",
		`{"email":"unverified@b.com","password":"Password123!"}`)

	w := testsupport.DoJSON(router, http.MethodPost, "/api/v1/auth/login",
		`{"email":"unverified@b.com","password":"Password123!"}`)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (%s)", w.Code, w.Body.String())
	}
	body := decode(t, w.Body.Bytes())
	if body["error"] != "email_not_verified" {
		t.Fatalf("error = %v, want email_not_verified", body["error"])
	}
}

func TestRefreshTokenUnverifiedUser(t *testing.T) {
	setupTest(t)
	// Signup no longer issues a session, so obtain one via verification and
	// then mark the account unverified again: RefreshToken must refuse it.
	_, refresh := signupSession(t, "unverified2@b.com")
	if _, err := testDB.Pool.Exec(context.Background(),
		`UPDATE users SET verified = false WHERE email = $1`, "unverified2@b.com"); err != nil {
		t.Fatalf("unverify: %v", err)
	}

	w := testsupport.DoJSON(router, http.MethodPost, "/api/v1/auth/refresh",
		`{"refresh_token":"`+refresh+`"}`)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (%s)", w.Code, w.Body.String())
	}
	if decode(t, w.Body.Bytes())["error"] != "email_not_verified" {
		t.Fatalf("expected email_not_verified error, got %s", w.Body.String())
	}
}

// Rate limiting (#15, #26): end-to-end proof, through a real gin.Engine +
// ratelimit.Limiter backed by real Redis (docker/docker-compose-test.yaml),
// that the x-rate-limit declared on /auth/login in auth.yaml actually trips a
// 429, and that it does not bleed into an operation with no x-rate-limit of
// its own.

func TestLoginRateLimited(t *testing.T) {
	setupTest(t)
	email := "ratelimit-login@b.com"
	testsupport.DoJSON(router, http.MethodPost, "/api/v1/auth/signup",
		`{"email":"`+email+`","password":"Password123!"}`)

	// login's x-rate-limit (scope: identifier, key: email) is requests=5,
	// burst=2 -> 7 requests allowed before the bucket is exhausted. A wrong
	// password still consumes a token (PerOperation runs before the handler),
	// so hammering with bad credentials drives the bucket to zero
	// deterministically without needing the real password.
	for i := 0; i < 7; i++ {
		w := testsupport.DoJSON(router, http.MethodPost, "/api/v1/auth/login",
			`{"email":"`+email+`","password":"wrong"}`)
		if w.Code == http.StatusTooManyRequests {
			t.Fatalf("request %d: unexpectedly rate limited within burst (status %d)", i, w.Code)
		}
	}

	w := testsupport.DoJSON(router, http.MethodPost, "/api/v1/auth/login",
		`{"email":"`+email+`","password":"wrong"}`)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 once the burst is exhausted (%s)", w.Code, w.Body.String())
	}
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("expected a Retry-After header on a 429 response")
	}

	// A non-rate-limited operation must never 429, even while /auth/login is
	// throttled for the same caller.
	me := testsupport.DoJSON(router, http.MethodGet, "/api/v1/auth/me", "")
	if me.Code == http.StatusTooManyRequests {
		t.Fatal("/auth/me must not be rate limited by /auth/login's bucket")
	}
}

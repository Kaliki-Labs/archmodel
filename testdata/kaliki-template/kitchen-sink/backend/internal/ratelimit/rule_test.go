package ratelimit_test

import (
	"testing"
	"time"

	"github.com/example/kitchen-sink-app/backend/internal/ratelimit"
)

func TestParseRule(t *testing.T) {
	tests := []struct {
		name    string
		spec    map[string]any
		want    ratelimit.Rule
		wantErr bool
	}{
		{
			name: "valid ip scope",
			spec: map[string]any{"scope": "ip", "requests": float64(10), "window": "60s", "burst": float64(5)},
			want: ratelimit.Rule{Scope: "ip", Requests: 10, Window: 60 * time.Second, Burst: 5},
		},
		{
			name: "valid identifier scope with key",
			spec: map[string]any{"scope": "identifier", "key": "email", "requests": float64(5), "window": "60s", "burst": float64(0)},
			want: ratelimit.Rule{Scope: "identifier", Key: "email", Requests: 5, Window: 60 * time.Second, Burst: 0},
		},
		{
			name: "valid custom scope",
			spec: map[string]any{"scope": "custom:workspace", "requests": float64(100), "window": "1m", "burst": float64(20)},
			want: ratelimit.Rule{Scope: "custom:workspace", Requests: 100, Window: time.Minute, Burst: 20},
		},
		{
			name: "plain int requests/burst (not just float64)",
			spec: map[string]any{"scope": "ip", "requests": 10, "window": "60s", "burst": 5},
			want: ratelimit.Rule{Scope: "ip", Requests: 10, Window: 60 * time.Second, Burst: 5},
		},
		{
			name:    "missing scope",
			spec:    map[string]any{"requests": float64(10), "window": "60s", "burst": float64(5)},
			wantErr: true,
		},
		{
			name:    "identifier scope missing key",
			spec:    map[string]any{"scope": "identifier", "requests": float64(5), "window": "60s", "burst": float64(0)},
			wantErr: true,
		},
		{
			name:    "missing requests",
			spec:    map[string]any{"scope": "ip", "window": "60s", "burst": float64(5)},
			wantErr: true,
		},
		{
			name:    "zero requests",
			spec:    map[string]any{"scope": "ip", "requests": float64(0), "window": "60s", "burst": float64(5)},
			wantErr: true,
		},
		{
			name:    "negative requests",
			spec:    map[string]any{"scope": "ip", "requests": float64(-1), "window": "60s", "burst": float64(5)},
			wantErr: true,
		},
		{
			name:    "malformed window",
			spec:    map[string]any{"scope": "ip", "requests": float64(10), "window": "not-a-duration", "burst": float64(5)},
			wantErr: true,
		},
		{
			name:    "missing window",
			spec:    map[string]any{"scope": "ip", "requests": float64(10), "burst": float64(5)},
			wantErr: true,
		},
		{
			name:    "zero window",
			spec:    map[string]any{"scope": "ip", "requests": float64(10), "window": "0s", "burst": float64(5)},
			wantErr: true,
		},
		{
			name:    "negative burst",
			spec:    map[string]any{"scope": "ip", "requests": float64(10), "window": "60s", "burst": float64(-1)},
			wantErr: true,
		},
		{
			name:    "missing burst",
			spec:    map[string]any{"scope": "ip", "requests": float64(10), "window": "60s"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ratelimit.ParseRule(tt.spec)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseRule(%v) = %+v, want error", tt.spec, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseRule(%v) unexpected error: %v", tt.spec, err)
			}
			if got != tt.want {
				t.Fatalf("ParseRule(%v) = %+v, want %+v", tt.spec, got, tt.want)
			}
		})
	}
}

func TestMustParseRule(t *testing.T) {
	t.Run("valid spec does not panic", func(t *testing.T) {
		got := ratelimit.MustParseRule(map[string]any{"scope": "ip", "requests": float64(10), "window": "60s", "burst": float64(5)})
		want := ratelimit.Rule{Scope: "ip", Requests: 10, Window: 60 * time.Second, Burst: 5}
		if got != want {
			t.Fatalf("MustParseRule = %+v, want %+v", got, want)
		}
	})

	t.Run("malformed spec panics", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatal("expected MustParseRule to panic on a malformed spec")
			}
		}()
		ratelimit.MustParseRule(map[string]any{"scope": "ip", "window": "not-a-duration"})
	})
}

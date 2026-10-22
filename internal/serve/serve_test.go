package serve

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Kaliki-Labs/archmodel/internal/model"
)

type fixture struct {
	srv    *Server
	ts     *httptest.Server
	dir    string
	builds atomic.Int32
	fail   atomic.Bool
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{dir: t.TempDir()}
	if err := os.WriteFile(filepath.Join(f.dir, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// An ignored directory must not trigger rebuilds.
	if err := os.MkdirAll(filepath.Join(f.dir, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	srv, err := New(Options{
		Build: func() (*model.Model, error) {
			n := f.builds.Add(1)
			if f.fail.Load() {
				return nil, errors.New("boom: syntax error")
			}
			return &model.Model{Version: 1, Warnings: []model.Warning{{Code: "build", Message: fmt.Sprint(n)}}}, nil
		},
		Render: func(m *model.Model) ([]byte, error) {
			return []byte("<html>build " + m.Warnings[0].Message + "</html>"), nil
		},
		Watch:        []string{f.dir},
		PollInterval: 5 * time.Millisecond,
		Debounce:     5 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	f.srv = srv
	f.ts = httptest.NewServer(srv.Handler())
	t.Cleanup(f.ts.Close)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go srv.Watch(ctx)
	return f
}

func (f *fixture) get(t *testing.T, path string) (string, http.Header) {
	t.Helper()
	res, err := http.Get(f.ts.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return string(b), res.Header
}

// touch changes a.go in a way the fingerprint always sees (size differs).
func (f *fixture) touch(t *testing.T, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(f.dir, "a.go"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// events opens /events, waits until subscribed and returns a channel of event
// names.
func (f *fixture) events(t *testing.T) <-chan string {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req, _ := http.NewRequestWithContext(ctx, "GET", f.ts.URL+"/events", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if ct := res.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type %q", ct)
	}
	out := make(chan string, 16)
	r := bufio.NewReader(res.Body)
	line, _ := r.ReadString('\n') // ": connected"
	if !strings.HasPrefix(line, ":") {
		t.Fatalf("first line %q", line)
	}
	go func() {
		defer res.Body.Close()
		for {
			l, err := r.ReadString('\n')
			if err != nil {
				return
			}
			if strings.HasPrefix(l, "event: ") {
				out <- strings.TrimSpace(strings.TrimPrefix(l, "event: "))
			}
		}
	}()
	return out
}

func waitEvent(t *testing.T, ch <-chan string, want string) {
	t.Helper()
	select {
	case got := <-ch:
		if got != want {
			t.Fatalf("event %q, want %q", got, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("no %q event", want)
	}
}

func TestServesPageAndModel(t *testing.T) {
	f := newFixture(t)
	page, h := f.get(t, "/")
	if page != "<html>build 1</html>" || !strings.HasPrefix(h.Get("Content-Type"), "text/html") {
		t.Errorf("page %q (%s)", page, h.Get("Content-Type"))
	}
	body, h := f.get(t, "/model.json")
	if !strings.Contains(body, `"version": 1`) || h.Get("Content-Type") != "application/json" {
		t.Errorf("model %q (%s)", body, h.Get("Content-Type"))
	}
	res, err := http.Get(f.ts.URL + "/nope")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 404 {
		t.Errorf("/nope: %d", res.StatusCode)
	}
}

func TestReloadAfterFileChange(t *testing.T) {
	f := newFixture(t)
	ev := f.events(t)
	// Noise in an ignored directory does not rebuild.
	os.WriteFile(filepath.Join(f.dir, "node_modules", "x.go"), []byte("package x"), 0o644)
	f.touch(t, "package a\n\nvar X = 1\n")
	waitEvent(t, ev, "reload")
	if page, _ := f.get(t, "/"); page != "<html>build 2</html>" {
		t.Errorf("page after reload: %q", page)
	}
	if n := f.builds.Load(); n != 2 {
		t.Errorf("builds = %d, want 2 (ignored dir must not rebuild)", n)
	}
}

func TestFailedRebuildKeepsLastGoodModel(t *testing.T) {
	f := newFixture(t)
	ev := f.events(t)
	f.fail.Store(true)
	f.touch(t, "package a\n\nbroken\n")
	waitEvent(t, ev, "builderror")
	if page, _ := f.get(t, "/"); page != "<html>build 1</html>" {
		t.Errorf("page after failed rebuild: %q", page)
	}
	// Fixing the source recovers.
	f.fail.Store(false)
	f.touch(t, "package a\n\nvar Fixed = true\n")
	waitEvent(t, ev, "reload")
	if page, _ := f.get(t, "/"); !strings.HasPrefix(page, "<html>build ") || page == "<html>build 1</html>" {
		t.Errorf("page after recovery: %q", page)
	}
}

func TestListenAndServeShutsDownWithOpenStream(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	addr := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		done <- f.srv.ListenAndServe(ctx, "127.0.0.1:0", func(a net.Addr) { addr <- a.String() })
	}()
	a := <-addr
	res, err := http.Get("http://" + a + "/events") // an SSE client that never hangs up
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	bufio.NewReader(res.Body).ReadString('\n')
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ListenAndServe did not return after cancel")
	}
}

func TestFirstBuildFailureIsAnError(t *testing.T) {
	_, err := New(Options{
		Build:  func() (*model.Model, error) { return nil, errors.New("no go.mod") },
		Render: func(*model.Model) ([]byte, error) { return nil, nil },
	})
	if err == nil || !strings.Contains(err.Error(), "no go.mod") {
		t.Fatalf("err = %v", err)
	}
}

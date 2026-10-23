// Package serve runs the Explorer over HTTP: it serves the page and the
// current model, watches the source tree, rebuilds on change and tells open
// pages to reload over Server-Sent Events (ADR-0009).
package serve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/Kaliki-Labs/archmodel/internal/model"
)

// Options configures a Server.
type Options struct {
	// Build extracts a fresh model. It is called once by New and again after
	// every detected change.
	Build func() (*model.Model, error)
	// Render turns a model into the Explorer HTML.
	Render func(*model.Model) ([]byte, error)
	// Watch lists the directories and files to watch.
	Watch []string
	// PollInterval is the fingerprint polling period (default 1s).
	PollInterval time.Duration
	// Debounce is how long to wait after a change before rebuilding, so a
	// burst of saves causes one rebuild (default 300ms).
	Debounce time.Duration
	// Log receives rebuild messages; nil discards them.
	Log io.Writer
}

// Server holds the last good model and its rendered page.
type Server struct {
	opts Options
	hub  *hub

	mu        sync.RWMutex
	modelJSON []byte
	page      []byte
}

// New builds the first model. It fails if that build fails, since there is
// nothing to serve yet.
func New(opts Options) (*Server, error) {
	if opts.PollInterval <= 0 {
		opts.PollInterval = time.Second
	}
	if opts.Debounce <= 0 {
		opts.Debounce = 300 * time.Millisecond
	}
	if opts.Log == nil {
		opts.Log = io.Discard
	}
	s := &Server{opts: opts, hub: newHub()}
	if err := s.rebuild(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Server) rebuild() error {
	m, err := s.opts.Build()
	if err != nil {
		return err
	}
	data, err := m.JSON()
	if err != nil {
		return err
	}
	page, err := s.opts.Render(m)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.modelJSON, s.page = data, page
	s.mu.Unlock()
	return nil
}

// Handler serves "/" (the Explorer), "/model.json" and "/events" (SSE).
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		s.mu.RLock()
		page := s.page
		s.mu.RUnlock()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(page)
	})
	mux.HandleFunc("GET /model.json", func(w http.ResponseWriter, r *http.Request) {
		s.mu.RLock()
		data := s.modelJSON
		s.mu.RUnlock()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(data)
	})
	mux.HandleFunc("GET /events", s.hub.serve)
	return mux
}

// Watch polls the watched paths until ctx is done. On a change it waits for
// the debounce, rebuilds and broadcasts "reload"; if the rebuild fails it keeps
// the last good model and broadcasts "builderror" instead.
func (s *Server) Watch(ctx context.Context) {
	last := fingerprint(s.opts.Watch)
	tick := time.NewTicker(s.opts.PollInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		if fingerprint(s.opts.Watch) == last {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(s.opts.Debounce):
		}
		// Take the baseline before building, so an edit made during the build
		// triggers another rebuild.
		last = fingerprint(s.opts.Watch)
		start := time.Now()
		if err := s.rebuild(); err != nil {
			fmt.Fprintf(s.opts.Log, "rebuild failed: %v\n", err)
			msg, _ := json.Marshal(map[string]string{"message": err.Error()})
			s.hub.broadcast("builderror", string(msg))
			continue
		}
		fmt.Fprintf(s.opts.Log, "rebuilt in %s\n", time.Since(start).Round(time.Millisecond))
		s.hub.broadcast("reload", "{}")
	}
}

// ListenAndServe listens on addr, calls ready with the bound address, and
// serves until ctx is done, then shuts down gracefully. It runs the watcher.
func (s *Server) ListenAndServe(ctx context.Context, addr string, ready func(net.Addr)) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	if ready != nil {
		ready(ln.Addr())
	}
	srv := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	wctx, stop := context.WithCancel(ctx)
	defer stop()
	go s.Watch(wctx)
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	// SSE streams never end on their own: close them, then shut down.
	s.hub.closeAll()
	sctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return nil
}

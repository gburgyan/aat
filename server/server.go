package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// ServerOptions configures the web server.
type ServerOptions struct {
	Port       int
	ArchiveDir string
	TracesDir  string
	DevMode    bool
	ViteURL    string // Vite dev server URL for dev proxy (default http://localhost:5173)
}

// Server is the AAT web API server.
type Server struct {
	opts         ServerOptions
	service      ArchiveService
	traceService *TraceService
	router       chi.Router
	httpServer   *http.Server
	mu           sync.Mutex
	addr         string
}

// NewServer creates a Server with the given options.
// Port defaults to 9119 if not set.
func NewServer(opts ServerOptions) *Server {
	if opts.Port == 0 {
		opts.Port = 9119
	}

	s := &Server{
		opts:         opts,
		service:      NewArchiveService(opts.ArchiveDir),
		traceService: NewTraceService(opts.TracesDir),
	}
	s.router = s.buildRouter()
	return s
}

// NewServerWithService creates a Server with a pre-built ArchiveService.
// This is used for static/in-memory archive viewing where no disk access is needed.
func NewServerWithService(opts ServerOptions, svc ArchiveService) *Server {
	if opts.Port == 0 {
		opts.Port = 9119
	}

	s := &Server{
		opts:         opts,
		service:      svc,
		traceService: NewTraceService(opts.TracesDir),
	}
	s.router = s.buildRouter()
	return s
}

func (s *Server) buildRouter() chi.Router {
	r := chi.NewRouter()

	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)
	if s.opts.DevMode {
		r.Use(middleware.Logger)
	}

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	r.Route("/api", func(r chi.Router) {
		r.Get("/runs", s.handleListRuns)
		r.Get("/runs/latest", s.handleLatestRun)
		r.Get("/runs/{id}", s.handleGetRun)
		r.Put("/runs/{id}/name", s.handleRenameRun)
		r.Delete("/runs/{id}/name", s.handleUnnameRun)
		r.Get("/runs/{id}/attempts/{attempt}", s.handleGetAttempt)
		r.Get("/runs/{id}/attempts/{attempt}/steps/{stepId}", s.handleGetAttemptStep)
		r.Get("/runs/{id}/steps/{stepId}", s.handleGetStep)
		r.Get("/runs/{id}/export", s.handleExportRun)

		r.Get("/batches", s.handleListBatches)
		r.Get("/batches/{id}", s.handleGetBatch)
		r.Put("/batches/{id}/name", s.handleRenameBatch)
		r.Delete("/batches/{id}/name", s.handleUnnameBatch)
		r.Get("/batches/{id}/export", s.handleExportBatch)

		r.Post("/import", s.handleImport)

		r.Get("/traces", s.handleListTraces)
		r.Get("/traces/{id}", s.handleGetTrace)
	})

	// Resolve /runs/latest to the actual run ID so the SPA has a real ID in the URL.
	r.Get("/runs/latest", s.handleLatestRunRedirect)

	// Catch-all: serve frontend (dev proxy or embedded SPA).
	if s.opts.DevMode {
		r.NotFound(s.devProxy())
	} else {
		r.NotFound(spaFileServer().ServeHTTP)
	}

	return r
}

// Handler returns the HTTP handler for use with httptest.
func (s *Server) Handler() http.Handler {
	return s.router
}

// Addr returns the actual listening address. Only valid after ListenAndServe has started.
func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addr
}

// ListenAndServe starts serving HTTP requests. It blocks until the server is
// shut down, at which point it returns http.ErrServerClosed.
func (s *Server) ListenAndServe() error {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", s.opts.Port))
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	srv := &http.Server{
		Handler:           s.router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	s.mu.Lock()
	s.addr = ln.Addr().String()
	s.httpServer = srv
	s.mu.Unlock()

	fmt.Fprintf(os.Stderr, "aat web: listening on http://localhost:%d\n", ln.Addr().(*net.TCPAddr).Port)

	return srv.Serve(ln)
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	srv := s.httpServer
	s.mu.Unlock()
	if srv == nil {
		return nil
	}
	return srv.Shutdown(ctx)
}

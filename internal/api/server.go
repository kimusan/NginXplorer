package api

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/kim/nginxplorer/internal/metrics"
	"github.com/kim/nginxplorer/web"
)

// Server is the main HTTP server for NginXplorer's web dashboard and API.
type Server struct {
	httpServer *http.Server
	store      *metrics.Store
	auth       *AuthManager
	handlers   *Handlers
	sseBroker  *SSEBroker
	bind       string
}

// ServerConfig holds configuration for the HTTP server.
type ServerConfig struct {
	Bind  string
	Auth  AuthConfig
	Store *metrics.Store
}

// NewServer creates a new HTTP server with all routes configured.
func NewServer(cfg ServerConfig) *Server {
	auth := NewAuthManager(cfg.Auth)
	handlers := NewHandlers(cfg.Store)
	sseBroker := NewSSEBroker(cfg.Store)

	s := &Server{
		store:     cfg.Store,
		auth:      auth,
		handlers:  handlers,
		sseBroker: sseBroker,
		bind:      cfg.Bind,
	}

	mux := http.NewServeMux()
	s.registerRoutes(mux)

	s.httpServer = &http.Server{
		Addr:         cfg.Bind,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 0, // SSE requires no write timeout
		IdleTimeout:  120 * time.Second,
	}

	return s
}

// registerRoutes sets up all HTTP routes.
func (s *Server) registerRoutes(mux *http.ServeMux) {
	// Auth endpoints (no auth middleware)
	mux.HandleFunc("/api/v1/auth/login", s.auth.HandleLogin)

	// Protected API endpoints
	protected := s.auth.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Route to the correct handler based on path
		switch r.URL.Path {
		case "/api/v1/auth/check":
			s.auth.HandleAuthCheck(w, r)
		case "/api/v1/metrics":
			s.handlers.HandleMetrics(w, r)
		case "/api/v1/vhosts":
			s.handlers.HandleVHosts(w, r)
		case "/api/v1/history":
			s.handlers.HandleHistory(w, r)
		case "/api/v1/stream":
			s.sseBroker.ServeHTTP(w, r)
		case "/api/v1/healthz":
			s.handlers.HandleHealthz(w, r)
		default:
			http.NotFound(w, r)
		}
	}))

	mux.Handle("/api/", protected)

	// Serve embedded web assets
	webFS, err := fs.Sub(web.Assets, ".")
	if err != nil {
		slog.Error("failed to create web filesystem", "error", err)
		return
	}

	fileServer := http.FileServer(http.FS(webFS))

	// Serve index.html for root and any non-API, non-file paths (SPA routing)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Try to serve a static file first
		if r.URL.Path != "/" {
			// Check if file exists in embedded FS
			f, err := webFS.Open(r.URL.Path[1:]) // strip leading /
			if err == nil {
				f.Close()
				fileServer.ServeHTTP(w, r)
				return
			}
		}

		// Serve index.html for root and SPA routes
		r.URL.Path = "/index.html"
		fileServer.ServeHTTP(w, r)
	})
}

// Start begins serving HTTP requests.
func (s *Server) Start() error {
	slog.Info("starting web dashboard",
		"bind", s.bind,
		"auth_enabled", s.auth.IsEnabled(),
	)

	if s.auth.IsEnabled() {
		slog.Info("authentication is enabled — login required for dashboard access")
	} else {
		slog.Warn("authentication is DISABLED — dashboard is accessible without login",
			"hint", "set auth.enabled=true in config for production use",
		)
	}

	fmt.Printf("\n  📡 NginXplorer dashboard: http://%s\n\n", s.bind)

	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("http server error: %w", err)
	}

	return nil
}

// Stop gracefully shuts down the HTTP server.
func (s *Server) Stop(ctx context.Context) error {
	slog.Info("shutting down web dashboard")
	return s.httpServer.Shutdown(ctx)
}

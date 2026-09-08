package api

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/kimusan/nginxplorer/internal/alerting"
	"github.com/kimusan/nginxplorer/internal/metrics"
	"github.com/kimusan/nginxplorer/internal/storage"
	"github.com/kimusan/nginxplorer/web"
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
	Bind        string
	Auth        AuthConfig
	Store       *metrics.Store
	SQLStore    *storage.SQLiteStore
	AlertEngine *alerting.Engine
}

// NewServer creates a new HTTP server with all routes configured.
func NewServer(cfg ServerConfig) *Server {
	auth := NewAuthManager(cfg.Auth)
	handlers := NewHandlers(cfg.Store, cfg.SQLStore, cfg.AlertEngine)
	sseBroker := NewSSEBroker(cfg.Store, cfg.AlertEngine)

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
		case "/api/v1/alerts":
			s.handlers.HandleAlerts(w, r)
		case "/api/v1/alerts/test":
			s.handlers.HandleTestAlert(w, r)
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

	// Serve static files and fallback to index.html for SPA routes
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		cleanPath := strings.TrimPrefix(r.URL.Path, "/")
		if cleanPath == "" || cleanPath == "index.html" {
			data, err := webFS.Open("index.html")
			if err != nil {
				http.Error(w, "index.html not found", http.StatusNotFound)
				return
			}
			defer data.Close()
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			http.ServeContent(w, r, "index.html", time.Time{}, data.(io.ReadSeeker))
			return
		}

		f, err := webFS.Open(cleanPath)
		if err != nil {
			// Fallback to index.html for SPA
			data, err := webFS.Open("index.html")
			if err != nil {
				http.NotFound(w, r)
				return
			}
			defer data.Close()
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			http.ServeContent(w, r, "index.html", time.Time{}, data.(io.ReadSeeker))
			return
		}
		defer f.Close()

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

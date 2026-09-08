package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/kim/nginxplorer/internal/metrics"
)

// Handlers holds the HTTP API route handlers.
type Handlers struct {
	store *metrics.Store
}

// NewHandlers creates a new Handlers instance.
func NewHandlers(store *metrics.Store) *Handlers {
	return &Handlers{store: store}
}

// HandleMetrics returns the current metrics snapshot as JSON.
// GET /api/v1/metrics
func (h *Handlers) HandleMetrics(w http.ResponseWriter, r *http.Request) {
	snapshot := h.store.Snapshot()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(snapshot); err != nil {
		slog.Error("failed to encode metrics", "error", err)
	}
}

// HandleVHosts returns the list of known vhosts.
// GET /api/v1/vhosts
func (h *Handlers) HandleVHosts(w http.ResponseWriter, r *http.Request) {
	names := h.store.VHostNames()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"vhosts": names,
	})
}

// HandleHistory returns historical time series data for a vhost.
// GET /api/v1/history?vhost=example.com&range=1h
func (h *Handlers) HandleHistory(w http.ResponseWriter, r *http.Request) {
	vhost := r.URL.Query().Get("vhost")
	rangeStr := r.URL.Query().Get("range")

	if vhost == "" {
		http.Error(w, "vhost parameter required", http.StatusBadRequest)
		return
	}

	duration := parseDuration(rangeStr)

	// If requesting "all" vhosts, aggregate
	if vhost == "all" || vhost == "_all" {
		names := h.store.VHostNames()
		allHistory := make(map[string]*metrics.VHostHistory)
		for _, name := range names {
			history := h.store.GetHistory(name, duration)
			if history != nil {
				allHistory[name] = history
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"vhosts":  names,
			"history": allHistory,
		})
		return
	}

	history := h.store.GetHistory(vhost, duration)
	if history == nil {
		http.Error(w, "vhost not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"vhost":   vhost,
		"history": history,
	})
}

// HandleHealthz is a simple health check endpoint.
// GET /api/v1/healthz
func (h *Handlers) HandleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"version": Version,
		"uptime":  time.Since(startTime).String(),
	})
}

// parseDuration converts a human-readable range string to a time.Duration.
func parseDuration(s string) time.Duration {
	switch s {
	case "1m", "1min":
		return 1 * time.Minute
	case "5m", "5min":
		return 5 * time.Minute
	case "15m", "15min":
		return 15 * time.Minute
	case "1h", "hour":
		return 1 * time.Hour
	case "6h":
		return 6 * time.Hour
	case "24h", "1d", "day":
		return 24 * time.Hour
	case "7d", "week":
		return 7 * 24 * time.Hour
	case "30d", "month":
		return 30 * 24 * time.Hour
	default:
		return 1 * time.Hour // Default to 1 hour
	}
}

// Version is set at build time via ldflags.
var Version = "dev"

var startTime = time.Now()

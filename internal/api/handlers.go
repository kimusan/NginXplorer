package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/kimusan/nginxplorer/internal/alerting"
	"github.com/kimusan/nginxplorer/internal/metrics"
	"github.com/kimusan/nginxplorer/internal/storage"
)

type Handlers struct {
	store       *metrics.Store
	sqlStore    *storage.SQLiteStore
	alertEngine *alerting.Engine
}

// NewHandlers creates a new Handlers instance.
func NewHandlers(store *metrics.Store, sqlStore *storage.SQLiteStore, alertEngine *alerting.Engine) *Handlers {
	return &Handlers{
		store:       store,
		sqlStore:    sqlStore,
		alertEngine: alertEngine,
	}
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
	now := time.Now()
	from := now.Add(-duration)

	var history *metrics.VHostHistory

	// First try querying SQLite storage if available
	if h.sqlStore != nil {
		var err error
		history, err = h.sqlStore.QueryHistory(r.Context(), vhost, from, now)
		if err != nil {
			slog.Debug("failed to query SQLite history, falling back to memory", "error", err)
		}
	}

	// Fallback to in-memory ring buffer if database returned no points or unavailable
	if history == nil || len(history.RPS) == 0 {
		if vhost == "all" || vhost == "_all" {
			names := h.store.VHostNames()
			agg := &metrics.VHostHistory{
				RPS:        make([]metrics.HistoryPoint, 0),
				LatencyP95: make([]metrics.HistoryPoint, 0),
				ErrorRate:  make([]metrics.HistoryPoint, 0),
				Bandwidth:  make([]metrics.HistoryPoint, 0),
				Summary:    &metrics.HistorySummary{},
			}
			for _, name := range names {
				vhHistory := h.store.GetHistory(name, duration)
				if vhHistory != nil {
					// Merge points
					for i, p := range vhHistory.RPS {
						if i < len(agg.RPS) {
							agg.RPS[i].Value += p.Value
						} else {
							agg.RPS = append(agg.RPS, p)
						}
					}
				}
			}
			history = agg
		} else {
			history = h.store.GetHistory(vhost, duration)
		}
	}

	if history == nil {
		history = &metrics.VHostHistory{
			RPS:        make([]metrics.HistoryPoint, 0),
			LatencyP95: make([]metrics.HistoryPoint, 0),
			ErrorRate:  make([]metrics.HistoryPoint, 0),
			Bandwidth:  make([]metrics.HistoryPoint, 0),
			Summary:    &metrics.HistorySummary{},
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(history)
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

// HandleAlerts returns the currently firing alerts and recent alert events.
// GET /api/v1/alerts
func (h *Handlers) HandleAlerts(w http.ResponseWriter, r *http.Request) {
	var active []alerting.AlertEvent
	var recent []alerting.AlertEvent

	if h.alertEngine != nil {
		active = h.alertEngine.ActiveAlerts()
		recent = h.alertEngine.RecentEvents(20)
	}

	if active == nil {
		active = make([]alerting.AlertEvent, 0)
	}
	if recent == nil {
		recent = make([]alerting.AlertEvent, 0)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"active": active,
		"recent": recent,
	})
}

// HandleTestAlert triggers a test notification across all configured channels.
// POST /api/v1/alerts/test
func (h *Handlers) HandleTestAlert(w http.ResponseWriter, r *http.Request) {
	if h.alertEngine == nil {
		http.Error(w, `{"error":"alerting engine not enabled"}`, http.StatusBadRequest)
		return
	}

	if err := h.alertEngine.SendTestNotification(r.Context()); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "test notification sent"})
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

package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/kimusan/nginxplorer/internal/metrics"
)

// SSEBroker manages Server-Sent Events connections to web clients.
// It subscribes to the metrics store and fans out snapshots to all
// connected browsers.
type SSEBroker struct {
	store *metrics.Store
}

// NewSSEBroker creates a new SSE broker.
func NewSSEBroker(store *metrics.Store) *SSEBroker {
	return &SSEBroker{store: store}
}

// ServeHTTP handles SSE connections from web clients.
// It sends an initial snapshot event followed by per-second metrics events.
func (b *SSEBroker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Ensure the response writer supports flushing
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable Nginx proxy buffering

	// Send initial snapshot with vhost list and current state
	snapshot := b.store.Snapshot()
	initialData := struct {
		VHosts  []string                          `json:"vhosts"`
		Current *metrics.Snapshot                 `json:"current"`
		History map[string]*metrics.VHostHistory  `json:"history"`
	}{
		VHosts:  b.store.VHostNames(),
		Current: snapshot,
		History: make(map[string]*metrics.VHostHistory),
	}

	// Include last hour of history for each vhost
	for _, name := range initialData.VHosts {
		history := b.store.GetHistory(name, 1*time.Hour)
		if history != nil {
			initialData.History[name] = history
		}
	}

	data, err := json.Marshal(initialData)
	if err != nil {
		slog.Error("failed to marshal initial snapshot", "error", err)
		return
	}

	fmt.Fprintf(w, "event: snapshot\ndata: %s\n\n", data)
	flusher.Flush()

	slog.Info("SSE client connected", "remote_addr", r.RemoteAddr)

	// Subscribe to real-time updates
	ch := b.store.Subscribe()
	defer func() {
		b.store.Unsubscribe(ch)
		slog.Info("SSE client disconnected", "remote_addr", r.RemoteAddr)
	}()

	// Send keepalive comment every 15 seconds to detect dead connections
	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()

	ctx := r.Context()

	for {
		select {
		case <-ctx.Done():
			return

		case snap, ok := <-ch:
			if !ok {
				return
			}

			data, err := json.Marshal(snap)
			if err != nil {
				slog.Error("failed to marshal snapshot", "error", err)
				continue
			}

			_, err = fmt.Fprintf(w, "event: metrics\ndata: %s\n\n", data)
			if err != nil {
				return // Client disconnected
			}
			flusher.Flush()

		case <-keepalive.C:
			_, err := fmt.Fprintf(w, ": keepalive\n\n")
			if err != nil {
				return // Client disconnected
			}
			flusher.Flush()
		}
	}
}

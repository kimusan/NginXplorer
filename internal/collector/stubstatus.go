package collector

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/kim/nginxplorer/internal/metrics"
)

// StubStatusCollector polls Nginx's stub_status endpoint.
type StubStatusCollector struct {
	url        string
	intervalMs int
	store      *metrics.Store
	client     *http.Client
}

// NewStubStatusCollector creates a new StubStatusCollector.
func NewStubStatusCollector(url string, intervalMs int, store *metrics.Store) *StubStatusCollector {
	return &StubStatusCollector{
		url:        url,
		intervalMs: intervalMs,
		store:      store,
		client: &http.Client{
			Timeout: 2 * time.Second,
		},
	}
}

// Start launches a goroutine polling on a ticker.
func (c *StubStatusCollector) Start(ctx context.Context) {
	if c.url == "" {
		return
	}
	ticker := time.NewTicker(time.Duration(c.intervalMs) * time.Millisecond)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.collect()
			}
		}
	}()
}

func (c *StubStatusCollector) collect() {
	resp, err := c.client.Get(c.url)
	if err != nil {
		slog.Error("stub_status request failed", "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Error("stub_status returned non-200", "status", resp.StatusCode)
		return
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Error("failed to read stub_status body", "error", err)
		return
	}

	status, err := parseStubStatus(string(body))
	if err != nil {
		slog.Error("failed to parse stub_status", "error", err)
		return
	}

	c.store.UpdateStubStatus(status)
}

func parseStubStatus(body string) (*metrics.StubStatus, error) {
	lines := strings.Split(strings.TrimSpace(body), "\n")
	if len(lines) < 4 {
		return nil, fmt.Errorf("invalid stub_status format")
	}

	status := &metrics.StubStatus{
		CollectedAt: time.Now(),
	}

	var active int64
	if _, err := fmt.Sscanf(strings.TrimSpace(lines[0]), "Active connections: %d", &active); err != nil {
		return nil, fmt.Errorf("failed to parse active connections: %w", err)
	}
	status.ActiveConnections = active

	var accepts, handled, requests int64
	if _, err := fmt.Sscanf(strings.TrimSpace(lines[2]), "%d %d %d", &accepts, &handled, &requests); err != nil {
		return nil, fmt.Errorf("failed to parse accepts/handled/requests: %w", err)
	}
	status.Accepts = accepts
	status.Handled = handled
	status.Requests = requests

	var reading, writing, waiting int64
	if _, err := fmt.Sscanf(strings.TrimSpace(lines[3]), "Reading: %d Writing: %d Waiting: %d", &reading, &writing, &waiting); err != nil {
		return nil, fmt.Errorf("failed to parse reading/writing/waiting: %w", err)
	}
	status.Reading = reading
	status.Writing = writing
	status.Waiting = waiting

	return status, nil
}

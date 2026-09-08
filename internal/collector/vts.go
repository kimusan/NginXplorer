package collector

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/kim/nginxplorer/internal/metrics"
)

// VTSCollector scrapes the nginx-module-vts JSON status endpoint.
type VTSCollector struct {
	url    string
	store  *metrics.Store
	client *http.Client
}

// NewVTSCollector creates a new VTSCollector.
func NewVTSCollector(url string, store *metrics.Store) *VTSCollector {
	return &VTSCollector{
		url:   url,
		store: store,
		client: &http.Client{
			Timeout: 2 * time.Second,
		},
	}
}

// Start starts polling the VTS JSON status endpoint every 1 second.
func (c *VTSCollector) Start(ctx context.Context) {
	if c.url == "" {
		return
	}
	ticker := time.NewTicker(1 * time.Second)
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

type vtsStatus struct {
	ServerZones map[string]vtsServerZone `json:"serverZones"`
}

type vtsServerZone struct {
	RequestCounter int64            `json:"requestCounter"`
	InBytes        int64            `json:"inBytes"`
	OutBytes       int64            `json:"outBytes"`
	Responses      map[string]int64 `json:"responses"`
	RequestMsec    float64          `json:"requestMsec"`
}

func (c *VTSCollector) collect() {
	resp, err := c.client.Get(c.url)
	if err != nil {
		slog.Error("VTS request failed", "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Error("VTS returned non-200", "status", resp.StatusCode)
		return
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Error("failed to read VTS body", "error", err)
		return
	}

	var status vtsStatus
	if err := json.Unmarshal(body, &status); err != nil {
		slog.Error("failed to parse VTS JSON", "error", err)
		return
	}
	
	// This is supplementary to log-based collection,
	// primarily useful for getting richer real-time data, but since the
	// store only takes log entries or stub_status right now, we just log
	// that we successfully fetched VTS data or process it accordingly.
	// We could extend store to handle VTS specifically, but for now we'll just log.
	slog.Debug("Collected VTS data", "zones", len(status.ServerZones))
}

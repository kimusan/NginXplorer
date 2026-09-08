package tui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kimusan/nginxplorer/internal/metrics"
)

func TestSSEClient_FetchHistory(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/history" {
			http.NotFound(w, r)
			return
		}
		vhost := r.URL.Query().Get("vhost")
		timeRange := r.URL.Query().Get("range")
		if vhost != "all" || timeRange != "1h" {
			t.Errorf("unexpected query params: vhost=%s range=%s", vhost, timeRange)
		}

		hist := metrics.VHostHistory{
			RPS: []metrics.HistoryPoint{{Timestamp: 1000, Value: 12.5}},
			Summary: &metrics.HistorySummary{
				TotalRequests: 1000,
				AvgRPS:        12.5,
				ErrorRate:     1.5,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(hist)
	}))
	defer ts.Close()

	client := NewSSEClient(ts.URL, "test-token", "", "")
	hist, err := client.FetchHistory("all", "1h")
	if err != nil {
		t.Fatalf("FetchHistory failed: %v", err)
	}

	if len(hist.RPS) != 1 || hist.RPS[0].Value != 12.5 {
		t.Errorf("unexpected RPS history: %+v", hist.RPS)
	}
	if hist.Summary == nil || hist.Summary.TotalRequests != 1000 {
		t.Errorf("unexpected summary: %+v", hist.Summary)
	}
}

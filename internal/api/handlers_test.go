package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/kimusan/nginxplorer/internal/metrics"
	"github.com/kimusan/nginxplorer/internal/storage"
)

func TestHandleHistory(t *testing.T) {
	store := metrics.NewStore(50, 100, 5)
	defer store.Stop()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	sqlStore, err := storage.NewSQLiteStore(dbPath, 7)
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}
	defer sqlStore.Close()

	// Populate SQLite store with test data
	now := time.Now().Truncate(time.Minute)
	err = sqlStore.Record(context.Background(), now.Add(-5*time.Minute), "example.com", metrics.VHostMetrics{
		RPS: 15,
		StatusCodes: metrics.StatusCodes{
			S2xx: 100,
			S5xx: 5,
		},
		Latency: metrics.LatencyStats{
			Avg: 12.5,
			P95: 25.0,
		},
	})
	if err != nil {
		t.Fatalf("failed to record metrics: %v", err)
	}

	handlers := NewHandlers(store, sqlStore, nil)

	// 1. Missing vhost param
	req := httptest.NewRequest("GET", "/api/v1/history", nil)
	w := httptest.NewRecorder()
	handlers.HandleHistory(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing vhost, got %d", w.Code)
	}

	// 2. Query existing vhost
	req = httptest.NewRequest("GET", "/api/v1/history?vhost=example.com&range=1h", nil)
	w = httptest.NewRecorder()
	handlers.HandleHistory(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var hist metrics.VHostHistory
	if err := json.NewDecoder(w.Body).Decode(&hist); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(hist.RPS) != 1 {
		t.Fatalf("expected 1 RPS point, got %d", len(hist.RPS))
	}
	if hist.Summary == nil {
		t.Fatalf("expected non-nil summary")
	}
	if hist.Summary.TotalRequests != 105 {
		t.Errorf("expected 105 total requests, got %d", hist.Summary.TotalRequests)
	}

	// 3. Fallback when vhost not in SQLite: query unknown vhost returns empty history struct, not error
	req = httptest.NewRequest("GET", "/api/v1/history?vhost=unknown.org&range=1h", nil)
	w = httptest.NewRecorder()
	handlers.HandleHistory(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	// 4. Test HandleAlerts
	req = httptest.NewRequest("GET", "/api/v1/alerts", nil)
	w = httptest.NewRecorder()
	handlers.HandleAlerts(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for alerts endpoint, got %d", w.Code)
	}
}

func TestStaticAssets_PWA(t *testing.T) {
	srv := NewServer(ServerConfig{
		Bind: ":0",
	})

	for _, path := range []string{"/manifest.json", "/sw.js", "/icon.svg", "/index.html"} {
		req := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		srv.httpServer.Handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200 for %s, got %d", path, w.Code)
		}
		if w.Body.Len() == 0 {
			t.Errorf("expected non-empty body for %s", path)
		}
	}
}

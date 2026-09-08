package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/kimusan/nginxplorer/internal/metrics"
)

func TestSQLiteStore_RecordAndQueryHistory(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewSQLiteStore(dbPath, 7)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	now := time.Now().Truncate(time.Minute)

	// Record metrics for site1
	vm1 := metrics.VHostMetrics{
		RPS:       10,
		ErrorRate: 20,
		StatusCodes: metrics.StatusCodes{
			S2xx: 80,
			S3xx: 0,
			S4xx: 15,
			S5xx: 5,
		},
		Latency: metrics.LatencyStats{
			Avg: 25.0,
			P95: 50.0,
			P99: 100.0,
		},
		Bandwidth: metrics.Bandwidth{
			In:  1024,
			Out: 4096,
		},
		UniqueVisitors: 42,
	}
	if err := store.Record(ctx, now.Add(-2*time.Minute), "site1.com", vm1); err != nil {
		t.Fatalf("failed to record vm1: %v", err)
	}

	// Record metrics for site2
	vm2 := metrics.VHostMetrics{
		RPS:       5,
		ErrorRate: 0,
		StatusCodes: metrics.StatusCodes{
			S2xx: 50,
			S3xx: 0,
			S4xx: 0,
			S5xx: 0,
		},
		Latency: metrics.LatencyStats{
			Avg: 10.0,
			P95: 20.0,
			P99: 30.0,
		},
		Bandwidth: metrics.Bandwidth{
			In:  512,
			Out: 2048,
		},
		UniqueVisitors: 15,
	}
	if err := store.Record(ctx, now.Add(-2*time.Minute), "site2.com", vm2); err != nil {
		t.Fatalf("failed to record vm2: %v", err)
	}

	// 1. Query site1 history
	hist1, err := store.QueryHistory(ctx, "site1.com", now.Add(-10*time.Minute), now)
	if err != nil {
		t.Fatalf("failed to query site1: %v", err)
	}
	if len(hist1.RPS) != 1 {
		t.Fatalf("expected 1 data point, got %d", len(hist1.RPS))
	}
	if hist1.Summary == nil {
		t.Fatalf("expected non-nil summary")
	}
	if hist1.Summary.TotalRequests != 100 {
		t.Errorf("expected 100 total requests, got %d", hist1.Summary.TotalRequests)
	}
	if hist1.Summary.StatusCodes.S4xx != 15 || hist1.Summary.StatusCodes.S5xx != 5 {
		t.Errorf("unexpected status codes: %+v", hist1.Summary.StatusCodes)
	}
	if hist1.Summary.ErrorRate != 20.0 {
		t.Errorf("expected 20%% error rate, got %.2f", hist1.Summary.ErrorRate)
	}
	if hist1.Summary.UniqueVisitors != 42 {
		t.Errorf("expected 42 unique visitors, got %d", hist1.Summary.UniqueVisitors)
	}

	// 2. Query aggregate "all" history
	histAll, err := store.QueryHistory(ctx, "all", now.Add(-10*time.Minute), now)
	if err != nil {
		t.Fatalf("failed to query all: %v", err)
	}
	if len(histAll.RPS) != 1 {
		t.Fatalf("expected 1 aggregated data point, got %d", len(histAll.RPS))
	}
	if histAll.Summary == nil {
		t.Fatalf("expected non-nil summary for all")
	}
	// Total requests: 100 (site1) + 50 (site2) = 150
	if histAll.Summary.TotalRequests != 150 {
		t.Errorf("expected 150 total requests, got %d", histAll.Summary.TotalRequests)
	}
	if histAll.Summary.StatusCodes.S2xx != 130 {
		t.Errorf("expected 130 2xx, got %d", histAll.Summary.StatusCodes.S2xx)
	}
	// Error rate: 20 / 150 = 13.333%
	if histAll.Summary.ErrorRate < 13.3 || histAll.Summary.ErrorRate > 13.4 {
		t.Errorf("expected ~13.33%% error rate, got %.2f", histAll.Summary.ErrorRate)
	}
	if histAll.Summary.TotalBytesIn != 1536 || histAll.Summary.TotalBytesOut != 6144 {
		t.Errorf("unexpected bytes in/out: in=%d out=%d", histAll.Summary.TotalBytesIn, histAll.Summary.TotalBytesOut)
	}
}

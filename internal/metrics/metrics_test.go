package metrics

import (
	"fmt"
	"math"
	"testing"
	"time"
)

func TestTDigest_BasicPercentiles(t *testing.T) {
	td := NewTDigest(100)

	// Add values 1 through 100
	for i := 1; i <= 100; i++ {
		td.Add(float64(i))
	}

	// Check percentiles (with some tolerance for approximation)
	p50 := td.Quantile(0.50)
	if math.Abs(p50-50) > 5 {
		t.Errorf("p50 = %f, want ~50", p50)
	}

	p95 := td.Quantile(0.95)
	if math.Abs(p95-95) > 5 {
		t.Errorf("p95 = %f, want ~95", p95)
	}

	p99 := td.Quantile(0.99)
	if math.Abs(p99-99) > 5 {
		t.Errorf("p99 = %f, want ~99", p99)
	}
}

func TestTDigest_Average(t *testing.T) {
	td := NewTDigest(100)

	for i := 1; i <= 100; i++ {
		td.Add(float64(i))
	}

	avg := td.Average()
	expected := 50.5
	if math.Abs(avg-expected) > 0.01 {
		t.Errorf("avg = %f, want %f", avg, expected)
	}
}

func TestTDigest_Empty(t *testing.T) {
	td := NewTDigest(100)

	if td.Quantile(0.5) != 0 {
		t.Error("expected 0 for empty digest")
	}
	if td.Average() != 0 {
		t.Error("expected 0 average for empty digest")
	}
	if td.Count() != 0 {
		t.Error("expected 0 count for empty digest")
	}
}

func TestTDigest_Reset(t *testing.T) {
	td := NewTDigest(100)
	td.Add(1)
	td.Add(2)
	td.Add(3)

	if td.Count() != 3 {
		t.Errorf("count = %f, want 3", td.Count())
	}

	td.Reset()

	if td.Count() != 0 {
		t.Errorf("count after reset = %f, want 0", td.Count())
	}
}

func TestTDigest_Stats(t *testing.T) {
	td := NewTDigest(100)

	// Add latency values in seconds (like Nginx $request_time)
	for i := 0; i < 100; i++ {
		td.Add(0.050) // 50ms
	}
	for i := 0; i < 10; i++ {
		td.Add(0.200) // 200ms (slower requests)
	}

	stats := td.Stats()

	// p50 should be around 50ms
	if stats.P50 < 30 || stats.P50 > 70 {
		t.Errorf("p50 = %fms, expected ~50ms", stats.P50)
	}

	// Average should be between 50 and 200
	if stats.Avg < 40 || stats.Avg > 80 {
		t.Errorf("avg = %fms, expected ~63ms", stats.Avg)
	}
}

func BenchmarkTDigest_Add(b *testing.B) {
	td := NewTDigest(100)
	for i := 0; i < b.N; i++ {
		td.Add(float64(i % 1000))
	}
}

func TestHyperLogLog_Basic(t *testing.T) {
	hll := NewHyperLogLog()

	// Add 1000 unique IPs
	for i := 0; i < 1000; i++ {
		hll.Add(fmt.Sprintf("192.168.%d.%d", i/256, i%256))
	}

	count := hll.Count()

	// HLL with 256 registers has ~6.5% error
	// So 1000 ± 100 is reasonable
	if count < 850 || count > 1150 {
		t.Errorf("count = %d, want ~1000 (±15%%)", count)
	}
}

func TestHyperLogLog_Duplicates(t *testing.T) {
	hll := NewHyperLogLog()

	// Add the same IP 1000 times
	for i := 0; i < 1000; i++ {
		hll.Add("192.168.1.1")
	}

	count := hll.Count()
	if count != 1 {
		t.Errorf("count = %d, want 1 (all duplicates)", count)
	}
}

func TestHyperLogLog_Empty(t *testing.T) {
	hll := NewHyperLogLog()
	count := hll.Count()
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
}

func TestWindowedHLL_Basic(t *testing.T) {
	whll := NewWindowedHLL(5) // 5 minute window

	now := int64(1000000)

	// Add IPs spread across time
	for i := 0; i < 100; i++ {
		whll.Add(fmt.Sprintf("10.0.0.%d", i), now)
	}

	count := whll.Count(now)
	if count < 80 || count > 120 {
		t.Errorf("count = %d, want ~100", count)
	}
}

func TestTopKTracker_Basic(t *testing.T) {
	tracker := NewTopKTracker(10)
	now := time.Now().Unix()

	// Add paths with different frequencies in current second
	for i := 0; i < 100; i++ {
		tracker.Add("/api/users", 0.05, true, now)
	}
	for i := 0; i < 50; i++ {
		tracker.Add("/api/orders", 0.1, true, now)
	}
	for i := 0; i < 10; i++ {
		tracker.Add("/health", 0.001, true, now)
	}

	top := tracker.Top(3, 5.0, now)

	if len(top) != 3 {
		t.Fatalf("expected 3 top paths, got %d", len(top))
	}

	// First should be /api/users (most frequent)
	if top[0].Path != "/api/users" {
		t.Errorf("top path = %s, want /api/users", top[0].Path)
	}
	// 100 requests over 5.0s window => 20 rps
	if top[0].RPS != 20.0 {
		t.Errorf("expected 20.0 rps, got %f", top[0].RPS)
	}

	// Second should be /api/orders
	if top[1].Path != "/api/orders" {
		t.Errorf("second path = %s, want /api/orders", top[1].Path)
	}
	if top[1].RPS != 10.0 {
		t.Errorf("expected 10.0 rps, got %f", top[1].RPS)
	}
}

func TestTopKTracker_WindowExpiration(t *testing.T) {
	tracker := NewTopKTracker(10)
	t0 := int64(1000)

	// Add 50 requests at t0
	for i := 0; i < 50; i++ {
		tracker.Add("/burst", 0.01, true, t0)
	}

	// At t0, should have 50 requests => 10 rps over 5s
	top0 := tracker.Top(1, 5.0, t0)
	if len(top0) != 1 || top0[0].RPS != 10.0 {
		t.Fatalf("expected 10.0 rps at t0, got %v", top0)
	}

	// At t0 + 10s (past 5s window), count should be 0
	top10 := tracker.Top(1, 5.0, t0+10)
	if len(top10) != 0 {
		t.Fatalf("expected 0 top paths after window expired, got %d", len(top10))
	}

	// At t0 + 65s (past 60s ring capacity), slot is completely recycled
	tracker.Add("/new", 0.01, true, t0+65)
	top65 := tracker.Top(2, 5.0, t0+65)
	if len(top65) != 1 || top65[0].Path != "/new" {
		t.Fatalf("expected only /new, got %v", top65)
	}
}

func TestStore_BotTrafficSegmentation(t *testing.T) {
	store := NewStore(10, 10, 5)
	now := time.Now()

	// Record 3 human requests
	for i := 0; i < 3; i++ {
		store.RecordEntry(&LogEntry{
			Host:       "example.com",
			RemoteAddr: "192.168.1.1",
			UserAgent:  "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/120.0.0.0 Safari/537.36",
			Status:     200,
			Timestamp:  now,
		})
	}

	// Record 2 good bot requests
	for i := 0; i < 2; i++ {
		store.RecordEntry(&LogEntry{
			Host:       "example.com",
			RemoteAddr: "66.249.66.1",
			UserAgent:  "Googlebot/2.1 (+http://www.google.com/bot.html)",
			Status:     200,
			Timestamp:  now,
		})
	}

	// Record 4 bad bot/scanner requests
	for i := 0; i < 4; i++ {
		store.RecordEntry(&LogEntry{
			Host:       "example.com",
			RemoteAddr: "45.155.205.233",
			UserAgent:  "sqlmap/1.6#stable",
			Status:     404,
			Timestamp:  now,
		})
	}

	// Commit the second
	store.commitSecond(now)

	snap := store.Snapshot()
	vh, ok := snap.VHosts["example.com"]
	if !ok {
		t.Fatalf("vhost example.com not found in snapshot")
	}

	if vh.BotTraffic.HumanRequests != 3 {
		t.Errorf("expected 3 human requests, got %d", vh.BotTraffic.HumanRequests)
	}
	if vh.BotTraffic.GoodBotRequests != 2 {
		t.Errorf("expected 2 good bot requests, got %d", vh.BotTraffic.GoodBotRequests)
	}
	if vh.BotTraffic.BadBotRequests != 4 {
		t.Errorf("expected 4 bad bot requests, got %d", vh.BotTraffic.BadBotRequests)
	}
}

func TestStore_GetHistory_Latency(t *testing.T) {
	store := NewStore(10, 10, 5)
	now := time.Now()

	store.RecordEntry(&LogEntry{
		Host:        "example.com",
		RemoteAddr:  "127.0.0.1",
		Status:      200,
		RequestTime: 0.050, // 50ms
		Timestamp:   now,
	})

	store.commitSecond(now)

	hist := store.GetHistory("example.com", 1*time.Minute)
	if hist == nil {
		t.Fatalf("expected non-nil history")
	}
	if len(hist.LatencyP95) != 1 {
		t.Fatalf("expected 1 latency point, got %d", len(hist.LatencyP95))
	}
	if hist.LatencyP95[0].Value != 50.0 {
		t.Errorf("expected latency 50.0ms, got %f", hist.LatencyP95[0].Value)
	}
}

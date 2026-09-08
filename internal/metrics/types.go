// Package metrics provides the core metrics types, in-memory store, and
// aggregation algorithms for NginXplorer.
package metrics

import (
	"sync"
	"time"
)

// LogEntry represents a single parsed access log line from Nginx.
type LogEntry struct {
	Timestamp    time.Time
	Host         string  // $server_name — the matched vhost
	RemoteAddr   string  // Client IP address
	Method       string  // HTTP method (GET, POST, etc.)
	URI          string  // Request URI (without query string if configured)
	Status       int     // HTTP status code
	BytesSent    int64   // Total bytes sent to client
	BodyBytes    int64   // Body bytes sent
	RequestLen   int64   // Request length in bytes
	RequestTime  float64 // Total request time in seconds
	UpstreamTime float64 // Upstream response time in seconds (may be 0)
	UpstreamAddr string  // Upstream server address
	UserAgent    string  // Client user agent
	Referer      string  // HTTP referer
}

// StubStatus represents the parsed output of Nginx's stub_status module.
type StubStatus struct {
	ActiveConnections int64
	Accepts           int64
	Handled           int64
	Requests          int64
	Reading           int64
	Writing           int64
	Waiting           int64
	CollectedAt       time.Time
}

// StatusCodes holds counters for each HTTP status code class.
type StatusCodes struct {
	S2xx int64 `json:"2xx"`
	S3xx int64 `json:"3xx"`
	S4xx int64 `json:"4xx"`
	S5xx int64 `json:"5xx"`
}

// Total returns the sum of all status code counters.
func (s StatusCodes) Total() int64 {
	return s.S2xx + s.S3xx + s.S4xx + s.S5xx
}

// LatencyStats holds latency percentile information.
type LatencyStats struct {
	P50 float64 `json:"p50"` // Median latency in ms
	P95 float64 `json:"p95"` // 95th percentile in ms
	P99 float64 `json:"p99"` // 99th percentile in ms
	Avg float64 `json:"avg"` // Average latency in ms
}

// Bandwidth holds traffic volume counters.
type Bandwidth struct {
	In  int64 `json:"in"`  // Bytes received
	Out int64 `json:"out"` // Bytes sent
}

// PathStats holds per-path aggregated metrics.
type PathStats struct {
	VHost      string  `json:"vhost,omitempty"`
	Path       string  `json:"path"`
	RPS        float64 `json:"rps"`
	AvgLatency float64 `json:"avg_latency"` // ms
	Status2xx  float64 `json:"status_2xx"`  // percentage
	Count      int64   `json:"-"`           // internal counter
	TotalTime  float64 `json:"-"`           // internal sum
	S2xxCount  int64   `json:"-"`           // internal counter
}

// VHostMetrics holds the current real-time metrics for a single virtual host.
type VHostMetrics struct {
	RPS            float64      `json:"rps"`
	ErrorRate      float64      `json:"error_rate"` // percentage of 4xx+5xx
	StatusCodes    StatusCodes  `json:"status_codes"`
	Latency        LatencyStats `json:"latency"`
	Bandwidth      Bandwidth    `json:"bandwidth"`
	UniqueVisitors int64        `json:"unique_visitors"`
	TopPaths       []PathStats  `json:"top_paths"`
}

// GlobalMetrics holds server-wide metrics from stub_status.
type GlobalMetrics struct {
	ActiveConnections int64 `json:"active_connections"`
	Reading           int64 `json:"reading"`
	Writing           int64 `json:"writing"`
	Waiting           int64 `json:"waiting"`
	Accepts           int64 `json:"accepts"`
	Handled           int64 `json:"handled"`
	Requests          int64 `json:"requests"`
}

// Snapshot represents a complete metrics snapshot at a point in time,
// sent to web/TUI clients via SSE.
type Snapshot struct {
	Timestamp time.Time               `json:"timestamp"`
	Global    GlobalMetrics           `json:"global"`
	VHosts    map[string]VHostMetrics `json:"vhosts"`
}

// HistoryPoint is a single data point in a historical time series.
type HistoryPoint struct {
	Timestamp int64   `json:"ts"`    // Unix timestamp
	Value     float64 `json:"value"` // Metric value
}

// VHostHistory holds historical time series data for a single vhost.
type VHostHistory struct {
	RPS       []HistoryPoint `json:"rps"`
	LatencyP95 []HistoryPoint `json:"latency_p95"`
	ErrorRate []HistoryPoint `json:"error_rate"`
	Bandwidth []HistoryPoint `json:"bandwidth"`
}

// HistorySnapshot is the full historical data sent on initial client connection.
type HistorySnapshot struct {
	VHosts  []string                    `json:"vhosts"`
	History map[string]*VHostHistory    `json:"history"`
}

// SecondBucket holds aggregated metrics for a single 1-second window.
type SecondBucket struct {
	Timestamp    time.Time
	Requests     int64
	StatusCodes  StatusCodes
	TotalLatency float64 // sum of all request times (for averaging)
	BytesIn      int64
	BytesOut     int64
}

// VHostState holds the live, mutable state for a single virtual host.
// It contains ring buffers of per-second buckets and streaming algorithms.
type VHostState struct {
	mu sync.RWMutex

	Name string

	// Ring buffer of per-second buckets (last 3600 seconds = 1 hour)
	Seconds     [3600]SecondBucket
	SecondsHead int // index of the current second

	// Current second accumulator (not yet committed to the ring)
	CurrentSecond SecondBucket

	// Streaming percentile calculation
	Digest *TDigest

	// Unique visitor counting (windowed for "active in last N minutes")
	Visitors *WindowedHLL

	// Top paths tracking (approximate frequency counting)
	PathCounts *TopKTracker

	// Historical RPS for trend comparison
	LastMinuteRPS float64
	Last5MinRPS   float64
}

// TopKTracker maintains approximate counts of the top-K most frequent items
// using a Count-Min Sketch inspired approach with a bounded heap.
type TopKTracker struct {
	mu       sync.Mutex
	counts   map[string]*pathCounter
	maxItems int
}

type pathCounter struct {
	count     int64
	totalTime float64
	s2xx      int64
}

// NewTopKTracker creates a new tracker that retains at most maxItems paths.
func NewTopKTracker(maxItems int) *TopKTracker {
	return &TopKTracker{
		counts:   make(map[string]*pathCounter),
		maxItems: maxItems,
	}
}

// Add records a request for the given path.
func (t *TopKTracker) Add(path string, latency float64, is2xx bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	pc, ok := t.counts[path]
	if !ok {
		// If we're at capacity, only add if this might be frequent
		if len(t.counts) >= t.maxItems*2 {
			t.evict()
		}
		pc = &pathCounter{}
		t.counts[path] = pc
	}

	pc.count++
	pc.totalTime += latency
	if is2xx {
		pc.s2xx++
	}
}

// evict removes the least frequent half of entries.
func (t *TopKTracker) evict() {
	if len(t.counts) == 0 {
		return
	}

	// Find median count
	var total int64
	for _, pc := range t.counts {
		total += pc.count
	}
	threshold := total / int64(len(t.counts))

	for path, pc := range t.counts {
		if pc.count <= threshold {
			delete(t.counts, path)
		}
	}
}

// Top returns the top N paths sorted by request count.
func (t *TopKTracker) Top(n int, windowSeconds float64) []PathStats {
	t.mu.Lock()
	defer t.mu.Unlock()

	if windowSeconds <= 0 {
		windowSeconds = 1
	}

	type entry struct {
		path string
		pc   *pathCounter
	}

	entries := make([]entry, 0, len(t.counts))
	for path, pc := range t.counts {
		entries = append(entries, entry{path, pc})
	}

	// Sort by count descending (simple insertion sort for small N)
	for i := 1; i < len(entries); i++ {
		for j := i; j > 0 && entries[j].pc.count > entries[j-1].pc.count; j-- {
			entries[j], entries[j-1] = entries[j-1], entries[j]
		}
	}

	if n > len(entries) {
		n = len(entries)
	}

	result := make([]PathStats, n)
	for i := 0; i < n; i++ {
		e := entries[i]
		avgLat := 0.0
		if e.pc.count > 0 {
			avgLat = (e.pc.totalTime / float64(e.pc.count)) * 1000 // convert to ms
		}
		s2xxPct := 0.0
		if e.pc.count > 0 {
			s2xxPct = float64(e.pc.s2xx) / float64(e.pc.count) * 100
		}
		result[i] = PathStats{
			Path:       e.path,
			RPS:        float64(e.pc.count) / windowSeconds,
			AvgLatency: avgLat,
			Status2xx:  s2xxPct,
			Count:      e.pc.count,
			TotalTime:  e.pc.totalTime,
			S2xxCount:  e.pc.s2xx,
		}
	}

	return result
}

// Reset clears all tracked paths (called on window rotation).
func (t *TopKTracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.counts = make(map[string]*pathCounter)
}

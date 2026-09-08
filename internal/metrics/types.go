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
	CountryCode  string  // 2-letter ISO country code e.g. "DK"
	CountryName  string  // Full country name e.g. "Denmark"
	CountryFlag  string  // Country flag emoji e.g. "🇩🇰"
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
	Count      int64   `json:"count"`       // total requests
	TotalTime  float64 `json:"-"`           // internal sum
	S2xxCount  int64   `json:"-"`           // internal counter
}

// CountryStats holds aggregated traffic metrics for a single country.
type CountryStats struct {
	CountryCode string  `json:"code"`       // 2-letter ISO code e.g. "DK"
	CountryName string  `json:"name"`       // Full country name e.g. "Denmark"
	Flag        string  `json:"flag"`       // Emoji flag e.g. "🇩🇰"
	RPS         float64 `json:"rps"`        // Requests per second in window
	Percentage  float64 `json:"percentage"` // Percentage of total traffic in window
	Count       int64   `json:"count"`      // Total requests in window
}

// VHostMetrics holds the current real-time metrics for a single virtual host.
type VHostMetrics struct {
	RPS            float64         `json:"rps"`
	ErrorRate      float64         `json:"error_rate"` // percentage of 4xx+5xx
	StatusCodes    StatusCodes     `json:"status_codes"`
	Latency        LatencyStats    `json:"latency"`
	LatencyBuckets []int64         `json:"latency_buckets"`
	Bandwidth      Bandwidth       `json:"bandwidth"`
	UniqueVisitors int64           `json:"unique_visitors"`
	BotTraffic     BotTrafficStats `json:"bot_traffic"`
	TopPaths       []PathStats     `json:"top_paths"`
	TopCountries   []CountryStats  `json:"top_countries"`
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

// HistorySummary holds aggregated totals for a historical query period.
type HistorySummary struct {
	TotalRequests  int64           `json:"total_requests"`
	AvgRPS         float64         `json:"avg_rps"`
	AvgLatency     float64         `json:"avg_latency"`
	ErrorRate      float64         `json:"error_rate"`
	StatusCodes    StatusCodes     `json:"status_codes"`
	TotalBytesIn   int64           `json:"total_bytes_in"`
	TotalBytesOut  int64           `json:"total_bytes_out"`
	UniqueVisitors int64           `json:"unique_visitors"`
	BotTraffic     BotTrafficStats `json:"bot_traffic,omitempty"`
	LatencyBuckets []int64         `json:"latency_buckets,omitempty"`
	TopCountries   []CountryStats  `json:"top_countries,omitempty"`
	TopPaths       []PathStats     `json:"top_paths,omitempty"`
}

// VHostHistory holds historical time series data for a single vhost.
type VHostHistory struct {
	RPS        []HistoryPoint  `json:"rps"`
	LatencyP95 []HistoryPoint  `json:"latency_p95"`
	ErrorRate  []HistoryPoint  `json:"error_rate"`
	Bandwidth  []HistoryPoint  `json:"bandwidth"`
	Summary    *HistorySummary `json:"summary,omitempty"`
}

// HistorySnapshot is the full historical data sent on initial client connection.
type HistorySnapshot struct {
	VHosts  []string                 `json:"vhosts"`
	History map[string]*VHostHistory `json:"history"`
}

// SecondBucket holds aggregated metrics for a single 1-second window.
type SecondBucket struct {
	Timestamp      time.Time
	Requests       int64
	StatusCodes    StatusCodes
	BotTraffic     BotTrafficStats
	LatencyBuckets [10]int64
	TotalLatency   float64 // sum of all request times (for averaging)
	BytesIn        int64
	BytesOut       int64
}

// LatencyBucketIndex maps a request duration (in seconds) to one of 10 histogram buckets:
// 0: <10ms, 1: 10-20ms, 2: 20-50ms, 3: 50-100ms, 4: 100-200ms,
// 5: 200-500ms, 6: 500ms-1s, 7: 1-2s, 8: 2-5s, 9: >5s.
func LatencyBucketIndex(reqTime float64) int {
	ms := reqTime * 1000.0
	switch {
	case ms < 10:
		return 0
	case ms < 20:
		return 1
	case ms < 50:
		return 2
	case ms < 100:
		return 3
	case ms < 200:
		return 4
	case ms < 500:
		return 5
	case ms < 1000:
		return 6
	case ms < 2000:
		return 7
	case ms < 5000:
		return 8
	default:
		return 9
	}
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

	// Country distribution tracking (rolling window)
	CountryCounts *CountryTracker

	// Historical RPS for trend comparison
	LastMinuteRPS float64
	Last5MinRPS   float64
}

// pathTrackerSlots defines the rolling memory window for path metrics (1 hour = 3600 seconds).
const pathTrackerSlots = 3600

// TopKTracker maintains rolling window counts of requested paths to calculate accurate req/s.
// It uses a 3600-second ring buffer of 1-second slot counters.
type TopKTracker struct {
	mu       sync.Mutex
	slots    [pathTrackerSlots]map[string]*pathCounter
	slotTime [pathTrackerSlots]int64
	maxItems int
}

type pathCounter struct {
	count     int64
	totalTime float64
	s2xx      int64
}

// NewTopKTracker creates a new tracker that retains at most maxItems paths over a rolling window.
func NewTopKTracker(maxItems int) *TopKTracker {
	t := &TopKTracker{
		maxItems: maxItems,
	}
	for i := range t.slots {
		t.slots[i] = make(map[string]*pathCounter)
	}
	return t
}

// Add records a request for the given path at the specified unix timestamp (in seconds).
// If unixTime <= 0, the current time is used.
func (t *TopKTracker) Add(path string, latency float64, is2xx bool, unixTime int64) {
	if unixTime <= 0 {
		unixTime = time.Now().Unix()
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	slotIdx := int(unixTime % pathTrackerSlots)
	if slotIdx < 0 {
		slotIdx = -slotIdx
	}

	// If this slot is from an older time, clear it for the current second
	if t.slotTime[slotIdx] != unixTime {
		t.slots[slotIdx] = make(map[string]*pathCounter)
		t.slotTime[slotIdx] = unixTime
	}

	slotMap := t.slots[slotIdx]
	pc, ok := slotMap[path]
	if !ok {
		// Bounded memory per slot
		if len(slotMap) >= t.maxItems*2 {
			t.evictSlot(slotMap)
		}
		pc = &pathCounter{}
		slotMap[path] = pc
	}

	pc.count++
	pc.totalTime += latency
	if is2xx {
		pc.s2xx++
	}
}

// evictSlot removes entries with count <= median in a single slot when at capacity.
func (t *TopKTracker) evictSlot(m map[string]*pathCounter) {
	if len(m) == 0 {
		return
	}
	var total int64
	for _, pc := range m {
		total += pc.count
	}
	threshold := total / int64(len(m))
	for path, pc := range m {
		if pc.count <= threshold {
			delete(m, path)
		}
	}
}

// Top returns the top N paths across the rolling window (up to pathTrackerSlots).
// If n <= 0, returns all active paths in the window.
func (t *TopKTracker) Top(n int, windowSeconds float64, nowUnix int64) []PathStats {
	t.mu.Lock()
	defer t.mu.Unlock()

	if nowUnix <= 0 {
		nowUnix = time.Now().Unix()
	}

	if windowSeconds <= 0 {
		windowSeconds = 10
	} else if windowSeconds > pathTrackerSlots {
		windowSeconds = pathTrackerSlots
	}

	aggregated := make(map[string]*pathCounter)
	lookback := int64(windowSeconds)
	for i := int64(0); i < lookback; i++ {
		sec := nowUnix - i
		slotIdx := int(sec % pathTrackerSlots)
		if slotIdx < 0 {
			slotIdx = -slotIdx
		}

		if t.slotTime[slotIdx] == sec {
			for path, pc := range t.slots[slotIdx] {
				agg, ok := aggregated[path]
				if !ok {
					agg = &pathCounter{}
					aggregated[path] = agg
				}
				agg.count += pc.count
				agg.totalTime += pc.totalTime
				agg.s2xx += pc.s2xx
			}
		}
	}

	type entry struct {
		path string
		pc   *pathCounter
	}

	entries := make([]entry, 0, len(aggregated))
	for path, pc := range aggregated {
		entries = append(entries, entry{path, pc})
	}

	// Sort by count descending
	for i := 1; i < len(entries); i++ {
		for j := i; j > 0 && entries[j].pc.count > entries[j-1].pc.count; j-- {
			entries[j], entries[j-1] = entries[j-1], entries[j]
		}
	}

	if n <= 0 || n > len(entries) {
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

// countryTrackerSlots defines the rolling memory window for country metrics (1 hour = 3600 seconds).
const countryTrackerSlots = 3600

// CountryTracker maintains rolling window counts of requests per country.
// It uses a 3600-second (1 hour) ring buffer of 1-second slot counters.
type CountryTracker struct {
	mu       sync.Mutex
	slots    [countryTrackerSlots]map[string]*countryCounter
	slotTime [countryTrackerSlots]int64
	names    map[string]string // code -> full country name
	flags    map[string]string // code -> emoji flag
}

type countryCounter struct {
	count int64
}

// NewCountryTracker creates a new country traffic tracker.
func NewCountryTracker() *CountryTracker {
	t := &CountryTracker{
		names: make(map[string]string),
		flags: make(map[string]string),
	}
	for i := range t.slots {
		t.slots[i] = make(map[string]*countryCounter)
	}
	return t
}

// Add records a request from a country at the specified unix timestamp.
func (t *CountryTracker) Add(code, name, flag string, unixTime int64) {
	if code == "" {
		return
	}
	if unixTime <= 0 {
		unixTime = time.Now().Unix()
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if _, ok := t.names[code]; !ok && name != "" {
		t.names[code] = name
	}
	if _, ok := t.flags[code]; !ok && flag != "" {
		t.flags[code] = flag
	}

	slotIdx := int(unixTime % countryTrackerSlots)
	if slotIdx < 0 {
		slotIdx = -slotIdx
	}

	if t.slotTime[slotIdx] != unixTime {
		t.slots[slotIdx] = make(map[string]*countryCounter)
		t.slotTime[slotIdx] = unixTime
	}

	slotMap := t.slots[slotIdx]
	cc, ok := slotMap[code]
	if !ok {
		cc = &countryCounter{}
		slotMap[code] = cc
	}
	cc.count++
}

// Top returns the top N countries across the rolling window (up to countryTrackerSlots).
// If n <= 0, returns all active countries in the window.
func (t *CountryTracker) Top(n int, windowSeconds float64, nowUnix int64) []CountryStats {
	t.mu.Lock()
	defer t.mu.Unlock()

	if windowSeconds <= 0 {
		windowSeconds = 60
	}
	if windowSeconds > countryTrackerSlots {
		windowSeconds = countryTrackerSlots
	}
	if nowUnix <= 0 {
		nowUnix = time.Now().Unix()
	}

	aggregated := make(map[string]int64)
	var grandTotal int64

	lookback := int64(windowSeconds)
	for i := int64(0); i < lookback; i++ {
		sec := nowUnix - i
		slotIdx := int(sec % countryTrackerSlots)
		if slotIdx < 0 {
			slotIdx = -slotIdx
		}

		if t.slotTime[slotIdx] == sec {
			for code, cc := range t.slots[slotIdx] {
				aggregated[code] += cc.count
				grandTotal += cc.count
			}
		}
	}

	type entry struct {
		code  string
		count int64
	}

	entries := make([]entry, 0, len(aggregated))
	for code, count := range aggregated {
		entries = append(entries, entry{code, count})
	}

	// Sort descending by count
	for i := 1; i < len(entries); i++ {
		for j := i; j > 0 && entries[j].count > entries[j-1].count; j-- {
			entries[j], entries[j-1] = entries[j-1], entries[j]
		}
	}

	if n <= 0 || n > len(entries) {
		n = len(entries)
	}

	result := make([]CountryStats, n)
	for i := 0; i < n; i++ {
		e := entries[i]
		pct := 0.0
		if grandTotal > 0 {
			pct = (float64(e.count) / float64(grandTotal)) * 100
		}
		result[i] = CountryStats{
			CountryCode: e.code,
			CountryName: t.names[e.code],
			Flag:        t.flags[e.code],
			RPS:         float64(e.count) / windowSeconds,
			Percentage:  pct,
			Count:       e.count,
		}
	}

	return result
}

// Reset clears all tracked paths in all rolling slots.
func (t *TopKTracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	for i := range t.slots {
		t.slots[i] = make(map[string]*pathCounter)
		t.slotTime[i] = 0
	}
}

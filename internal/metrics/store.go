package metrics

import (
	"sync"
	"time"
)

// Store is the central metrics aggregation engine. It receives log entries
// and stub_status updates from collectors, maintains per-vhost sliding-window
// aggregations, and produces snapshots for the API/SSE layer.
type Store struct {
	mu sync.RWMutex

	// Per-vhost state
	vhosts map[string]*VHostState

	// Global metrics from stub_status
	global     GlobalMetrics
	prevStatus StubStatus // for computing deltas

	// Configuration
	maxVHosts    int
	maxTopPaths  int
	visitorMins  int

	// Subscribers for real-time updates
	subMu       sync.RWMutex
	subscribers map[chan *Snapshot]struct{}

	// Ticker for per-second aggregation
	done chan struct{}
}

// NewStore creates a new metrics store with the given configuration.
func NewStore(maxVHosts, maxTopPaths, visitorWindowMinutes int) *Store {
	if maxVHosts <= 0 {
		maxVHosts = 50
	}
	if maxTopPaths <= 0 {
		maxTopPaths = 100
	}
	if visitorWindowMinutes <= 0 {
		visitorWindowMinutes = 5
	}

	s := &Store{
		vhosts:      make(map[string]*VHostState),
		maxVHosts:   maxVHosts,
		maxTopPaths: maxTopPaths,
		visitorMins: visitorWindowMinutes,
		subscribers: make(map[chan *Snapshot]struct{}),
		done:        make(chan struct{}),
	}

	return s
}

// Start begins the per-second aggregation ticker.
func (s *Store) Start() {
	go s.aggregationLoop()
}

// Stop halts the aggregation loop.
func (s *Store) Stop() {
	close(s.done)
}

// aggregationLoop runs every second, committing the current second's
// accumulated data into the ring buffer and broadcasting a snapshot.
func (s *Store) aggregationLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.done:
			return
		case now := <-ticker.C:
			s.commitSecond(now)
			snapshot := s.Snapshot()
			s.broadcast(snapshot)
		}
	}
}

// RecordEntry processes a single access log entry, updating the
// corresponding vhost's metrics.
func (s *Store) RecordEntry(entry *LogEntry) {
	if entry.Host == "" {
		entry.Host = "_default"
	}

	s.mu.Lock()
	vh, ok := s.vhosts[entry.Host]
	if !ok {
		if len(s.vhosts) >= s.maxVHosts {
			s.mu.Unlock()
			return // Drop entries from unknown vhosts when at capacity
		}
		vh = s.newVHostState(entry.Host)
		s.vhosts[entry.Host] = vh
	}
	s.mu.Unlock()

	vh.mu.Lock()
	defer vh.mu.Unlock()

	// Update current second accumulator
	vh.CurrentSecond.Requests++
	vh.CurrentSecond.TotalLatency += entry.RequestTime
	vh.CurrentSecond.BytesIn += entry.RequestLen
	vh.CurrentSecond.BytesOut += entry.BytesSent

	// Status code classification
	switch {
	case entry.Status >= 200 && entry.Status < 300:
		vh.CurrentSecond.StatusCodes.S2xx++
	case entry.Status >= 300 && entry.Status < 400:
		vh.CurrentSecond.StatusCodes.S3xx++
	case entry.Status >= 400 && entry.Status < 500:
		vh.CurrentSecond.StatusCodes.S4xx++
	case entry.Status >= 500:
		vh.CurrentSecond.StatusCodes.S5xx++
	}

	// Latency tracking (T-Digest)
	vh.Digest.Add(entry.RequestTime)

	// Unique visitor tracking (HyperLogLog)
	vh.Visitors.Add(entry.RemoteAddr, entry.Timestamp.Unix())

	// Client bot and crawler classification
	switch ClassifyClient(entry.UserAgent) {
	case ClientGoodBot:
		vh.CurrentSecond.BotTraffic.GoodBotRequests++
	case ClientBadBot:
		vh.CurrentSecond.BotTraffic.BadBotRequests++
	default:
		vh.CurrentSecond.BotTraffic.HumanRequests++
	}

	// Path tracking
	is2xx := entry.Status >= 200 && entry.Status < 300
	vh.PathCounts.Add(entry.URI, entry.RequestTime, is2xx, entry.Timestamp.Unix())
}

// UpdateStubStatus updates the global metrics from a stub_status poll.
func (s *Store) UpdateStubStatus(status *StubStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.global = GlobalMetrics{
		ActiveConnections: status.ActiveConnections,
		Reading:           status.Reading,
		Writing:           status.Writing,
		Waiting:           status.Waiting,
		Accepts:           status.Accepts,
		Handled:           status.Handled,
		Requests:          status.Requests,
	}
	s.prevStatus = *status
}

// commitSecond finalizes the current 1-second bucket and rotates the ring.
func (s *Store) commitSecond(now time.Time) {
	s.mu.RLock()
	vhosts := make([]*VHostState, 0, len(s.vhosts))
	for _, vh := range s.vhosts {
		vhosts = append(vhosts, vh)
	}
	s.mu.RUnlock()

	for _, vh := range vhosts {
		vh.mu.Lock()

		// Commit current second to ring buffer
		vh.CurrentSecond.Timestamp = now
		vh.Seconds[vh.SecondsHead] = vh.CurrentSecond
		vh.SecondsHead = (vh.SecondsHead + 1) % len(vh.Seconds)

		// Reset current second accumulator
		vh.CurrentSecond = SecondBucket{}

		vh.mu.Unlock()
	}
}

// Snapshot creates a complete metrics snapshot for all vhosts.
func (s *Store) Snapshot() *Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now()
	snap := &Snapshot{
		Timestamp: now,
		Global:    s.global,
		VHosts:    make(map[string]VHostMetrics, len(s.vhosts)),
	}

	for name, vh := range s.vhosts {
		snap.VHosts[name] = s.computeVHostMetrics(vh, now)
	}

	return snap
}

// computeVHostMetrics calculates the current metrics for a vhost
// by aggregating its ring buffer of per-second buckets.
func (s *Store) computeVHostMetrics(vh *VHostState, now time.Time) VHostMetrics {
	vh.mu.RLock()
	defer vh.mu.RUnlock()

	// Aggregate last 5 seconds for RPS (smoothed)
	windowSecs := 5
	var totalReqs int64
	var totalStatusCodes StatusCodes
	var totalBytesIn, totalBytesOut int64
	validSeconds := 0

	for i := 0; i < windowSecs; i++ {
		idx := (vh.SecondsHead - 1 - i + len(vh.Seconds)) % len(vh.Seconds)
		bucket := vh.Seconds[idx]
		if bucket.Timestamp.IsZero() {
			continue
		}
		age := now.Sub(bucket.Timestamp)
		if age > time.Duration(windowSecs+2)*time.Second {
			continue
		}

		validSeconds++
		totalReqs += bucket.Requests
		totalStatusCodes.S2xx += bucket.StatusCodes.S2xx
		totalStatusCodes.S3xx += bucket.StatusCodes.S3xx
		totalStatusCodes.S4xx += bucket.StatusCodes.S4xx
		totalStatusCodes.S5xx += bucket.StatusCodes.S5xx
		totalBytesIn += bucket.BytesIn
		totalBytesOut += bucket.BytesOut
	}

	// Aggregate bot traffic over rolling 60 seconds for stable segmentation
	botWindowSecs := 60
	var totalBotTraffic BotTrafficStats
	for i := 0; i < botWindowSecs; i++ {
		idx := (vh.SecondsHead - 1 - i + len(vh.Seconds)) % len(vh.Seconds)
		bucket := vh.Seconds[idx]
		if bucket.Timestamp.IsZero() {
			continue
		}
		if now.Sub(bucket.Timestamp) > time.Duration(botWindowSecs+2)*time.Second {
			continue
		}
		totalBotTraffic.HumanRequests += bucket.BotTraffic.HumanRequests
		totalBotTraffic.GoodBotRequests += bucket.BotTraffic.GoodBotRequests
		totalBotTraffic.BadBotRequests += bucket.BotTraffic.BadBotRequests
	}

	rps := 0.0
	if validSeconds > 0 {
		rps = float64(totalReqs) / float64(validSeconds)
	}

	errorRate := 0.0
	totalCodes := totalStatusCodes.Total()
	if totalCodes > 0 {
		errorRate = float64(totalStatusCodes.S4xx+totalStatusCodes.S5xx) / float64(totalCodes) * 100
	}

	nowUnix := now.Unix()
	topPaths := vh.PathCounts.Top(10, float64(windowSecs), nowUnix)
	for i := range topPaths {
		topPaths[i].VHost = vh.Name
	}
	visitors := vh.Visitors.Count(nowUnix)

	return VHostMetrics{
		RPS:            rps,
		ErrorRate:      errorRate,
		StatusCodes:    totalStatusCodes,
		Latency:        vh.Digest.Stats(),
		Bandwidth:      Bandwidth{In: totalBytesIn, Out: totalBytesOut},
		UniqueVisitors: visitors,
		BotTraffic:     totalBotTraffic,
		TopPaths:       topPaths,
	}
}

// newVHostState creates a fresh VHostState with initialized sub-structures.
func (s *Store) newVHostState(name string) *VHostState {
	return &VHostState{
		Name:       name,
		Digest:     NewTDigest(100),
		Visitors:   NewWindowedHLL(s.visitorMins),
		PathCounts: NewTopKTracker(s.maxTopPaths),
	}
}

// Subscribe creates a new channel that receives snapshots every second.
// The caller must eventually call Unsubscribe to prevent leaks.
func (s *Store) Subscribe() chan *Snapshot {
	ch := make(chan *Snapshot, 10) // buffered to avoid blocking
	s.subMu.Lock()
	s.subscribers[ch] = struct{}{}
	s.subMu.Unlock()
	return ch
}

// Unsubscribe removes a subscriber channel and closes it.
func (s *Store) Unsubscribe(ch chan *Snapshot) {
	s.subMu.Lock()
	delete(s.subscribers, ch)
	s.subMu.Unlock()
	close(ch)
}

// broadcast sends a snapshot to all subscribers, dropping slow consumers.
func (s *Store) broadcast(snap *Snapshot) {
	s.subMu.RLock()
	defer s.subMu.RUnlock()

	for ch := range s.subscribers {
		select {
		case ch <- snap:
		default:
			// Slow consumer — drop this update
		}
	}
}

// VHostNames returns a sorted list of all known vhost names.
func (s *Store) VHostNames() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	names := make([]string, 0, len(s.vhosts))
	for name := range s.vhosts {
		names = append(names, name)
	}
	return names
}

// GetHistory returns historical time series data for a vhost over the
// specified duration. Data comes from the in-memory ring buffer.
func (s *Store) GetHistory(vhost string, duration time.Duration) *VHostHistory {
	s.mu.RLock()
	vh, ok := s.vhosts[vhost]
	s.mu.RUnlock()

	if !ok {
		return nil
	}

	vh.mu.RLock()
	defer vh.mu.RUnlock()

	now := time.Now()
	cutoff := now.Add(-duration)

	history := &VHostHistory{
		RPS:        make([]HistoryPoint, 0),
		LatencyP95: make([]HistoryPoint, 0),
		ErrorRate:  make([]HistoryPoint, 0),
		Bandwidth:  make([]HistoryPoint, 0),
	}

	// Walk the ring buffer to extract time series
	for i := 0; i < len(vh.Seconds); i++ {
		idx := (vh.SecondsHead - 1 - i + len(vh.Seconds)) % len(vh.Seconds)
		bucket := vh.Seconds[idx]
		if bucket.Timestamp.IsZero() {
			continue
		}
		if bucket.Timestamp.Before(cutoff) {
			break
		}

		ts := bucket.Timestamp.Unix()
		total := bucket.StatusCodes.Total()

		history.RPS = append(history.RPS, HistoryPoint{
			Timestamp: ts,
			Value:     float64(bucket.Requests),
		})

		// Approximate error rate from this second
		errRate := 0.0
		if total > 0 {
			errRate = float64(bucket.StatusCodes.S4xx+bucket.StatusCodes.S5xx) / float64(total) * 100
		}
		history.ErrorRate = append(history.ErrorRate, HistoryPoint{
			Timestamp: ts,
			Value:     errRate,
		})

		history.Bandwidth = append(history.Bandwidth, HistoryPoint{
			Timestamp: ts,
			Value:     float64(bucket.BytesOut),
		})
	}

	// Reverse to chronological order
	reverseHistoryPoints(history.RPS)
	reverseHistoryPoints(history.ErrorRate)
	reverseHistoryPoints(history.Bandwidth)

	return history
}

func reverseHistoryPoints(pts []HistoryPoint) {
	for i, j := 0, len(pts)-1; i < j; i, j = i+1, j-1 {
		pts[i], pts[j] = pts[j], pts[i]
	}
}

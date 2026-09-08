package metrics

import (
	"math"
	"sort"
	"sync"
)

// TDigest implements a streaming approximate percentile algorithm.
// It maintains a compressed representation of a distribution that allows
// accurate percentile queries (p50, p95, p99) without storing all values.
//
// Based on the T-Digest algorithm by Ted Dunning.
// Reference: https://github.com/tdunning/t-digest
type TDigest struct {
	mu          sync.Mutex
	centroids   []centroid
	count       float64
	compression float64
	maxSize     int
	totalSum    float64 // sum of all values (for computing average)
}

// centroid represents a cluster of nearby values.
type centroid struct {
	mean  float64
	count float64
}

// NewTDigest creates a new T-Digest with the given compression factor.
// Higher compression = more accuracy but more memory.
// Recommended: 100 for general use, 200 for high accuracy.
func NewTDigest(compression float64) *TDigest {
	if compression <= 0 {
		compression = 100
	}
	return &TDigest{
		centroids:   make([]centroid, 0, int(compression)*2),
		compression: compression,
		maxSize:     int(compression) * 5,
	}
}

// Add records a single value into the digest.
func (td *TDigest) Add(value float64) {
	td.mu.Lock()
	defer td.mu.Unlock()

	td.addCentroid(centroid{mean: value, count: 1})
	td.totalSum += value

	if len(td.centroids) > td.maxSize {
		td.compress()
	}
}

// addCentroid inserts a centroid maintaining sorted order by mean.
func (td *TDigest) addCentroid(c centroid) {
	td.count += c.count

	// Binary search for insertion point
	idx := sort.Search(len(td.centroids), func(i int) bool {
		return td.centroids[i].mean >= c.mean
	})

	// Insert at idx
	td.centroids = append(td.centroids, centroid{})
	copy(td.centroids[idx+1:], td.centroids[idx:])
	td.centroids[idx] = c
}

// compress merges centroids to keep the digest within size bounds.
// Uses a running cumulative sum for O(n) complexity instead of O(n²).
func (td *TDigest) compress() {
	if len(td.centroids) <= 1 {
		return
	}

	// Sort by mean (should already be sorted, but ensure)
	sort.Slice(td.centroids, func(i, j int) bool {
		return td.centroids[i].mean < td.centroids[j].mean
	})

	result := make([]centroid, 0, len(td.centroids))
	result = append(result, td.centroids[0])

	// Running cumulative count for O(n) quantile computation
	cumBefore := 0.0                                    // count before current result centroid
	incomingCum := td.centroids[0].count                // running cumulative for incoming centroids

	for i := 1; i < len(td.centroids); i++ {
		current := &result[len(result)-1]
		incoming := td.centroids[i]

		// Quantile position: use cumulative counts (O(1) per iteration)
		qCurrent := (current.count/2 + cumBefore) / td.count
		qIncoming := (incoming.count/2 + incomingCum) / td.count

		// Limit function: max cluster size depends on quantile (tighter at tails)
		limitCurrent := td.maxClusterSize(qCurrent)
		limitIncoming := td.maxClusterSize(qIncoming)
		limit := math.Min(limitCurrent, limitIncoming)

		if current.count+incoming.count <= limit {
			// Merge: weighted average
			totalCount := current.count + incoming.count
			current.mean = (current.mean*current.count + incoming.mean*incoming.count) / totalCount
			current.count = totalCount
		} else {
			cumBefore += current.count
			result = append(result, incoming)
		}
		incomingCum += incoming.count
	}

	td.centroids = result
}

// maxClusterSize returns the maximum number of values a centroid at quantile q
// can contain. Centroids near the tails (q≈0 or q≈1) have smaller limits
// for higher accuracy at extreme percentiles.
func (td *TDigest) maxClusterSize(q float64) float64 {
	return 4 * td.count * q * (1 - q) / td.compression
}

// Quantile returns the estimated value at the given quantile (0.0 to 1.0).
// For example, Quantile(0.95) returns the 95th percentile.
func (td *TDigest) Quantile(q float64) float64 {
	td.mu.Lock()
	defer td.mu.Unlock()

	if len(td.centroids) == 0 {
		return 0
	}

	if len(td.centroids) == 1 {
		return td.centroids[0].mean
	}

	if q <= 0 {
		return td.centroids[0].mean
	}
	if q >= 1 {
		return td.centroids[len(td.centroids)-1].mean
	}

	// Target rank
	target := q * td.count

	var cumulative float64
	for i, c := range td.centroids {
		lower := cumulative
		upper := cumulative + c.count
		mid := (lower + upper) / 2

		if target < mid {
			if i == 0 {
				return c.mean
			}
			// Interpolate between previous and current centroid
			prev := td.centroids[i-1]
			prevMid := (cumulative - prev.count + cumulative) / 2
			fraction := (target - prevMid) / (mid - prevMid)
			return prev.mean + fraction*(c.mean-prev.mean)
		}

		cumulative += c.count
	}

	return td.centroids[len(td.centroids)-1].mean
}

// Count returns the total number of values added.
func (td *TDigest) Count() float64 {
	td.mu.Lock()
	defer td.mu.Unlock()
	return td.count
}

// Average returns the mean of all added values.
func (td *TDigest) Average() float64 {
	td.mu.Lock()
	defer td.mu.Unlock()
	if td.count == 0 {
		return 0
	}
	return td.totalSum / td.count
}

// Reset clears all data from the digest.
func (td *TDigest) Reset() {
	td.mu.Lock()
	defer td.mu.Unlock()
	td.centroids = td.centroids[:0]
	td.count = 0
	td.totalSum = 0
}

// Stats returns the latency percentiles in milliseconds.
// Input values are assumed to be in seconds (as Nginx $request_time provides).
func (td *TDigest) Stats() LatencyStats {
	return LatencyStats{
		P50: td.Quantile(0.50) * 1000, // Convert seconds to ms
		P95: td.Quantile(0.95) * 1000,
		P99: td.Quantile(0.99) * 1000,
		Avg: td.Average() * 1000,
	}
}

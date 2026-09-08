package metrics

import (
	"hash/fnv"
	"math"
	"math/bits"
	"sync"
)

// HyperLogLog implements the HyperLogLog algorithm for approximate
// cardinality estimation (counting unique visitors).
//
// It uses ~1.5KB of memory (256 registers) and provides estimates
// with a standard error of ~6.5%.
//
// Reference: Flajolet, P., et al. "HyperLogLog: the analysis of a
// near-optimal cardinality estimation algorithm" (2007).
type HyperLogLog struct {
	mu        sync.Mutex
	registers [256]uint8 // 2^8 = 256 registers
	p         uint       // precision (8 bits = 256 registers)
}

// NewHyperLogLog creates a new HyperLogLog counter.
func NewHyperLogLog() *HyperLogLog {
	return &HyperLogLog{
		p: 8, // 256 registers, ~6.5% standard error
	}
}

// Add records an item (typically a client IP address).
func (h *HyperLogLog) Add(item string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	hash := h.hash(item)

	// Use first p bits as register index
	idx := hash >> (64 - h.p)

	// Count leading zeros in the remaining (64-p) bits, plus 1
	remaining := hash << h.p
	var rho uint8
	if remaining == 0 {
		rho = uint8(64-h.p) + 1
	} else {
		rho = uint8(bits.LeadingZeros64(remaining)) + 1
	}

	if rho > h.registers[idx] {
		h.registers[idx] = rho
	}
}

// Count returns the estimated number of unique items added.
func (h *HyperLogLog) Count() int64 {
	h.mu.Lock()
	defer h.mu.Unlock()

	m := float64(len(h.registers)) // 256
	alpha := h.alpha(m)

	// Compute harmonic mean
	var sum float64
	zeros := 0
	for _, val := range h.registers {
		sum += math.Pow(2, -float64(val))
		if val == 0 {
			zeros++
		}
	}

	estimate := alpha * m * m / sum

	// Small range correction
	if estimate <= 2.5*m && zeros > 0 {
		// Linear counting
		estimate = m * math.Log(m/float64(zeros))
	}

	// Large range correction (for 64-bit hash)
	twoTo64 := math.Pow(2, 64)
	if estimate > twoTo64/30 {
		estimate = -twoTo64 * math.Log(1-estimate/twoTo64)
	}

	return int64(math.Round(estimate))
}

// Reset clears all registers.
func (h *HyperLogLog) Reset() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.registers = [256]uint8{}
}

// hash produces a well-distributed 64-bit hash of the input string.
// Uses FNV-1a as the base hash, then applies a splitmix64 finalizer
// for full avalanche (every input bit affects every output bit).
func (h *HyperLogLog) hash(s string) uint64 {
	hasher := fnv.New64a()
	hasher.Write([]byte(s))
	x := hasher.Sum64()

	// splitmix64 finalizer — excellent avalanche properties
	x ^= x >> 30
	x *= 0xbf58476d1ce4e5b9
	x ^= x >> 27
	x *= 0x94d049bb133111eb
	x ^= x >> 31

	return x
}

// alpha returns the bias correction constant for m registers.
func (h *HyperLogLog) alpha(m float64) float64 {
	switch m {
	case 16:
		return 0.673
	case 32:
		return 0.697
	case 64:
		return 0.709
	default:
		return 0.7213 / (1 + 1.079/m)
	}
}

// WindowedHLL maintains separate HyperLogLog counters for time windows,
// allowing "unique visitors in the last N minutes" queries.
type WindowedHLL struct {
	mu      sync.Mutex
	windows []*windowSlot
	slotDur int // slot duration in seconds
	slots   int // number of slots
}

type windowSlot struct {
	hll       *HyperLogLog
	startTime int64 // unix timestamp
}

// NewWindowedHLL creates a windowed HLL that covers windowMinutes using
// 1-minute slots.
func NewWindowedHLL(windowMinutes int) *WindowedHLL {
	if windowMinutes <= 0 {
		windowMinutes = 5
	}
	slots := windowMinutes
	windows := make([]*windowSlot, slots)
	for i := range windows {
		windows[i] = &windowSlot{
			hll: NewHyperLogLog(),
		}
	}
	return &WindowedHLL{
		windows: windows,
		slotDur: 60, // 1-minute slots
		slots:   slots,
	}
}

// Add records a visitor IP for the given timestamp.
func (w *WindowedHLL) Add(ip string, unixTime int64) {
	w.mu.Lock()
	defer w.mu.Unlock()

	slotIdx := int(unixTime/int64(w.slotDur)) % w.slots
	slot := w.windows[slotIdx]

	expectedStart := (unixTime / int64(w.slotDur)) * int64(w.slotDur)
	if slot.startTime != expectedStart {
		// New time window — reset this slot
		slot.hll.Reset()
		slot.startTime = expectedStart
	}

	slot.hll.Add(ip)
}

// Count returns the estimated unique visitors across all active windows.
// This is an approximation since we union separate HLLs.
func (w *WindowedHLL) Count(nowUnix int64) int64 {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Merge all active slots into a combined HLL
	combined := NewHyperLogLog()
	cutoff := nowUnix - int64(w.slots*w.slotDur)

	for _, slot := range w.windows {
		if slot.startTime >= cutoff {
			// Merge registers (take max of each register)
			for i, val := range slot.hll.registers {
				if val > combined.registers[i] {
					combined.registers[i] = val
				}
			}
		}
	}

	return combined.Count()
}

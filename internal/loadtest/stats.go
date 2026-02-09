package loadtest

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Stats collects latency samples, counts, and byte counters during a load test.
// Latency slices are protected by a mutex; counts and byte counters use atomics.
type Stats struct {
	mu             sync.Mutex
	connectLatency []time.Duration
	rtLatency      []time.Duration
	errorBreakdown map[string]int

	successCount  atomic.Int64
	errorCount    atomic.Int64
	bytesSent     atomic.Int64
	bytesReceived atomic.Int64
	startTime     time.Time
}

// Result holds the computed statistics from a Stats snapshot.
type Result struct {
	Scenario       string
	Duration       time.Duration
	Concurrency    int
	TotalRequests  int64
	RequestsPerSec float64
	BytesSent      int64
	BytesReceived  int64
	ThroughputMBps float64

	ConnectP50 time.Duration
	ConnectP95 time.Duration
	ConnectP99 time.Duration

	RoundTripP50 time.Duration
	RoundTripP95 time.Duration
	RoundTripP99 time.Duration
	RoundTripMax time.Duration

	ErrorCount     int64
	ErrorRate      float64
	ErrorBreakdown map[string]int
}

// Start records the load test start time.
func (s *Stats) Start() {
	s.mu.Lock()
	s.startTime = time.Now()
	s.errorBreakdown = make(map[string]int)
	s.mu.Unlock()
}

// RecordConnect records a connection establishment latency sample.
func (s *Stats) RecordConnect(d time.Duration) {
	s.mu.Lock()
	s.connectLatency = append(s.connectLatency, d)
	s.mu.Unlock()
}

// RecordRoundTrip records a full round-trip latency sample.
func (s *Stats) RecordRoundTrip(d time.Duration) {
	s.mu.Lock()
	s.rtLatency = append(s.rtLatency, d)
	s.mu.Unlock()
}

// RecordSuccess increments the successful request counter.
func (s *Stats) RecordSuccess() {
	s.successCount.Add(1)
}

// RecordError increments the error counter and tracks the error string.
func (s *Stats) RecordError(errStr string) {
	s.errorCount.Add(1)
	s.mu.Lock()
	s.errorBreakdown[errStr]++
	s.mu.Unlock()
}

// RecordBytes adds to the cumulative bytes sent and received counters.
func (s *Stats) RecordBytes(sent, received int64) {
	s.bytesSent.Add(sent)
	s.bytesReceived.Add(received)
}

// Snapshot computes and returns a Result from the current stats.
// It copies and sorts latency slices so the caller gets a consistent view.
func (s *Stats) Snapshot(scenario string, concurrency int) Result {
	s.mu.Lock()
	duration := time.Since(s.startTime)

	// Copy slices to avoid holding the lock during sort.
	connectCopy := make([]time.Duration, len(s.connectLatency))
	copy(connectCopy, s.connectLatency)
	rtCopy := make([]time.Duration, len(s.rtLatency))
	copy(rtCopy, s.rtLatency)

	// Copy error breakdown.
	errBreakdown := make(map[string]int, len(s.errorBreakdown))
	for k, v := range s.errorBreakdown {
		errBreakdown[k] = v
	}
	s.mu.Unlock()

	sort.Slice(connectCopy, func(i, j int) bool { return connectCopy[i] < connectCopy[j] })
	sort.Slice(rtCopy, func(i, j int) bool { return rtCopy[i] < rtCopy[j] })

	success := s.successCount.Load()
	errors := s.errorCount.Load()
	total := success + errors
	sent := s.bytesSent.Load()
	received := s.bytesReceived.Load()

	secs := duration.Seconds()
	var rps float64
	var throughput float64
	if secs > 0 {
		rps = float64(total) / secs
		throughput = float64(sent+received) / (1024 * 1024) / secs
	}

	var errRate float64
	if total > 0 {
		errRate = float64(errors) / float64(total)
	}

	var rtMax time.Duration
	if len(rtCopy) > 0 {
		rtMax = rtCopy[len(rtCopy)-1]
	}

	return Result{
		Scenario:       scenario,
		Duration:       duration,
		Concurrency:    concurrency,
		TotalRequests:  total,
		RequestsPerSec: rps,
		BytesSent:      sent,
		BytesReceived:  received,
		ThroughputMBps: throughput,

		ConnectP50: percentile(connectCopy, 0.50),
		ConnectP95: percentile(connectCopy, 0.95),
		ConnectP99: percentile(connectCopy, 0.99),

		RoundTripP50: percentile(rtCopy, 0.50),
		RoundTripP95: percentile(rtCopy, 0.95),
		RoundTripP99: percentile(rtCopy, 0.99),
		RoundTripMax: rtMax,

		ErrorCount:     errors,
		ErrorRate:      errRate,
		ErrorBreakdown: errBreakdown,
	}
}

// percentile returns the value at the given percentile p (0.0–1.0) from a
// pre-sorted slice. Returns zero if the slice is empty.
func percentile(sorted []time.Duration, p float64) time.Duration {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	idx := int(float64(n-1) * p)
	if idx >= n {
		idx = n - 1
	}
	return sorted[idx]
}

/*
 * Centralized metrics collection, aggregation, and smoothing layer.
 *
 * Solves:
 *   #2  Single Metric Dependency — tracks queue depth, P95 latency, utilization, error rate, RPS
 *   #5  Raw Metric Noise         — EWMA smoothing + rolling windows + sustained-duration checks
 *   #13 No Metrics Layer         — decouples autoscaler from raw router state
 */

package lib

import (
	"math"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// EWMA (Exponentially Weighted Moving Average)
// ---------------------------------------------------------------------------

// EWMA provides exponentially weighted moving average smoothing.
// Alpha controls responsiveness: higher alpha = faster reaction, more noise.
type EWMA struct {
	alpha float64
	value float64
	set   bool
}

func NewEWMA(alpha float64) *EWMA {
	return &EWMA{alpha: alpha}
}

func (e *EWMA) Add(sample float64) {
	if !e.set {
		e.value = sample
		e.set = true
		return
	}
	e.value = e.alpha*sample + (1-e.alpha)*e.value
}

func (e *EWMA) Value() float64 {
	return e.value
}

func (e *EWMA) Reset() {
	e.value = 0
	e.set = false
}

// ---------------------------------------------------------------------------
// Rolling Window
// ---------------------------------------------------------------------------

// RollingWindow keeps the last N float64 samples for percentile/avg calculations.
type RollingWindow struct {
	data []float64
	size int
	pos  int
	full bool
}

func NewRollingWindow(size int) *RollingWindow {
	return &RollingWindow{
		data: make([]float64, size),
		size: size,
	}
}

func (rw *RollingWindow) Add(v float64) {
	rw.data[rw.pos] = v
	rw.pos++
	if rw.pos >= rw.size {
		rw.pos = 0
		rw.full = true
	}
}

func (rw *RollingWindow) Len() int {
	if rw.full {
		return rw.size
	}
	return rw.pos
}

func (rw *RollingWindow) Values() []float64 {
	n := rw.Len()
	out := make([]float64, n)
	if rw.full {
		copy(out, rw.data[rw.pos:])
		copy(out[rw.size-rw.pos:], rw.data[:rw.pos])
	} else {
		copy(out, rw.data[:rw.pos])
	}
	return out
}

// Percentile returns the p-th percentile (0-100) of the window contents.
func (rw *RollingWindow) Percentile(p float64) float64 {
	vals := rw.Values()
	n := len(vals)
	if n == 0 {
		return 0
	}
	// Sort a copy for percentile calculation
	sorted := make([]float64, n)
	copy(sorted, vals)
	sortFloat64s(sorted)

	rank := p / 100.0 * float64(n-1)
	lower := int(math.Floor(rank))
	upper := int(math.Ceil(rank))
	if lower == upper || upper >= n {
		return sorted[lower]
	}
	frac := rank - float64(lower)
	return sorted[lower]*(1-frac) + sorted[upper]*frac
}

func (rw *RollingWindow) Avg() float64 {
	vals := rw.Values()
	if len(vals) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range vals {
		sum += v
	}
	return sum / float64(len(vals))
}

// Simple insertion sort — windows are small (typically ≤200 samples).
func sortFloat64s(a []float64) {
	for i := 1; i < len(a); i++ {
		key := a[i]
		j := i - 1
		for j >= 0 && a[j] > key {
			a[j+1] = a[j]
			j--
		}
		a[j+1] = key
	}
}

// ---------------------------------------------------------------------------
// ClusterMetrics — the centralized metrics snapshot consumed by the autoscaler
// ---------------------------------------------------------------------------

// ClusterMetrics is a point-in-time view of smoothed cluster-wide metrics.
type ClusterMetrics struct {
	// Smoothed values (EWMA)
	QueueDepth        float64 // pending requests waiting for a worker
	P95Latency        float64 // 95th-percentile response time (seconds)
	WorkerUtilization float64 // avg active_requests / max_concurrency across workers
	ErrorRate         float64 // errors / total requests in the window
	RequestsPerSec    float64 // throughput

	// Raw counters for context
	TotalWorkers   int
	HealthyWorkers int
	InFlight       int
	QueueSize      int

	// Trend
	RPSTrend float64 // positive = growing, negative = shrinking

	CollectedAt time.Time
}

// ---------------------------------------------------------------------------
// MetricsCollector — goroutine-safe collector that feeds the autoscaler
// ---------------------------------------------------------------------------

// MetricsCollectorConfig controls smoothing and window behavior.
type MetricsCollectorConfig struct {
	EWMAAlpha         float64       // smoothing factor (0.0–1.0), default 0.3
	LatencyWindowSize int           // rolling window size for latency percentiles, default 200
	RPSWindowSize     int           // rolling window for RPS trend detection, default 30
	CollectInterval   time.Duration // how often the collector samples, default 1s
	MaxConcurrency    int           // per-worker max concurrency for utilization calc, default 50
}

func DefaultMetricsCollectorConfig() MetricsCollectorConfig {
	return MetricsCollectorConfig{
		EWMAAlpha:         0.3,
		LatencyWindowSize: 200,
		RPSWindowSize:     30,
		CollectInterval:   1 * time.Second,
		MaxConcurrency:    WorkerMaxConcurrency,
	}
}

// MetricsCollector is the centralized metrics layer that aggregates and smooths raw signals.
type MetricsCollector struct {
	mu   sync.RWMutex
	cfg  MetricsCollectorConfig
	snap ClusterMetrics

	// EWMA smoothers
	queueEWMA       *EWMA
	latencyEWMA     *EWMA
	utilizationEWMA *EWMA
	errorRateEWMA   *EWMA
	rpsEWMA         *EWMA

	// Rolling windows
	latencyWindow *RollingWindow
	rpsWindow     *RollingWindow

	// Counters for RPS/error rate derivation
	totalRequests int64
	totalErrors   int64
	prevRequests  int64
	prevErrors    int64
	prevTime      time.Time

	// Pending request queue size (fed externally)
	queueSize int
}

func NewMetricsCollector(cfg MetricsCollectorConfig) *MetricsCollector {
	return &MetricsCollector{
		cfg:             cfg,
		queueEWMA:       NewEWMA(cfg.EWMAAlpha),
		latencyEWMA:     NewEWMA(cfg.EWMAAlpha),
		utilizationEWMA: NewEWMA(cfg.EWMAAlpha),
		errorRateEWMA:   NewEWMA(cfg.EWMAAlpha),
		rpsEWMA:         NewEWMA(cfg.EWMAAlpha),
		latencyWindow:   NewRollingWindow(cfg.LatencyWindowSize),
		rpsWindow:       NewRollingWindow(cfg.RPSWindowSize),
		prevTime:        time.Now(),
	}
}

// RecordLatency records a single request latency sample (in seconds).
func (mc *MetricsCollector) RecordLatency(seconds float64) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	mc.latencyWindow.Add(seconds)
	mc.latencyEWMA.Add(seconds)
}

// RecordRequest increments the total request counter.
func (mc *MetricsCollector) RecordRequest() {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	mc.totalRequests++
}

// RecordError increments the total error counter.
func (mc *MetricsCollector) RecordError() {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	mc.totalErrors++
}

// SetQueueSize updates the current pending queue size.
func (mc *MetricsCollector) SetQueueSize(size int) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	mc.queueSize = size
}

// Collect gathers all raw signals from the router and produces a smoothed snapshot.
// Call this periodically (e.g., every CollectInterval).
func (mc *MetricsCollector) Collect(router *Router) ClusterMetrics {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(mc.prevTime).Seconds()
	if elapsed <= 0 {
		elapsed = 1
	}

	// --- RPS ---
	deltaReqs := mc.totalRequests - mc.prevRequests
	rps := float64(deltaReqs) / elapsed
	mc.rpsEWMA.Add(rps)
	mc.rpsWindow.Add(rps)

	// --- Error rate ---
	deltaErrors := mc.totalErrors - mc.prevErrors
	var errRate float64
	if deltaReqs > 0 {
		errRate = float64(deltaErrors) / float64(deltaReqs)
	}
	mc.errorRateEWMA.Add(errRate)

	mc.prevRequests = mc.totalRequests
	mc.prevErrors = mc.totalErrors
	mc.prevTime = now

	// --- Queue depth ---
	mc.queueEWMA.Add(float64(mc.queueSize))

	// --- Worker utilization ---
	router.workersM.RLock()
	totalWorkers := len(router.workers)
	healthyWorkers := 0
	totalActive := int64(0)
	for _, w := range router.workers {
		if w.GetLifecycleState() == StateHealthy {
			healthyWorkers++
			totalActive += w.ActiveRequests()
		}
	}
	router.workersM.RUnlock()

	var utilization float64
	if healthyWorkers > 0 && mc.cfg.MaxConcurrency > 0 {
		utilization = float64(totalActive) / float64(int64(healthyWorkers)*int64(mc.cfg.MaxConcurrency))
	}
	mc.utilizationEWMA.Add(utilization)

	// --- P95 latency ---
	p95 := mc.latencyWindow.Percentile(95)

	// --- RPS trend (linear regression slope over the rolling window) ---
	rpsTrend := mc.computeRPSTrend()

	inFlight := router.InFlightCount()

	mc.snap = ClusterMetrics{
		QueueDepth:        mc.queueEWMA.Value(),
		P95Latency:        p95,
		WorkerUtilization: mc.utilizationEWMA.Value(),
		ErrorRate:         mc.errorRateEWMA.Value(),
		RequestsPerSec:    mc.rpsEWMA.Value(),

		TotalWorkers:   totalWorkers,
		HealthyWorkers: healthyWorkers,
		InFlight:       inFlight,
		QueueSize:      mc.queueSize,

		RPSTrend: rpsTrend,

		CollectedAt: now,
	}

	return mc.snap
}

// Snapshot returns the last collected metrics without recalculating.
func (mc *MetricsCollector) Snapshot() ClusterMetrics {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	return mc.snap
}

// computeRPSTrend calculates a simple linear regression slope over the RPS rolling window.
// Positive means traffic is growing; negative means traffic is shrinking.
func (mc *MetricsCollector) computeRPSTrend() float64 {
	vals := mc.rpsWindow.Values()
	n := len(vals)
	if n < 3 {
		return 0
	}

	// Simple OLS slope: β = (n*Σ(xi*yi) - Σxi*Σyi) / (n*Σ(xi²) - (Σxi)²)
	var sumX, sumY, sumXY, sumX2 float64
	for i, y := range vals {
		x := float64(i)
		sumX += x
		sumY += y
		sumXY += x * y
		sumX2 += x * x
	}
	nf := float64(n)
	denom := nf*sumX2 - sumX*sumX
	if denom == 0 {
		return 0
	}
	return (nf*sumXY - sumX*sumY) / denom
}

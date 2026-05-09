/*
 * Request admission control layer.
 *
 * Solves:
 *   #12 No Request Admission Control — per-worker concurrency limits, request queuing, backpressure
 *
 * The AdmissionController sits in front of the router and gates incoming requests.
 * It enforces a global concurrency limit and uses a bounded queue for backpressure.
 */

package lib

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"master/monitoring"
)

var (
	ErrAdmissionQueueFull = errors.New("request queue is full, try again later")
	ErrAdmissionTimeout   = errors.New("request timed out waiting for admission")
)

// AdmissionConfig controls admission behavior.
type AdmissionConfig struct {
	MaxConcurrencyPerWorker int           // maximum concurrent requests per worker
	MaxQueueSize            int           // maximum pending requests in queue
	QueueTimeout            time.Duration // how long a request can wait in queue
}

// DefaultAdmissionConfig returns sensible defaults.
func DefaultAdmissionConfig() AdmissionConfig {
	return AdmissionConfig{
		MaxConcurrencyPerWorker: 50,
		MaxQueueSize:            500,
		QueueTimeout:            30 * time.Second,
	}
}

// AdmissionController gates incoming requests with concurrency limits and queuing.
type AdmissionController struct {
	cfg        AdmissionConfig
	active     atomic.Int64
	maxActive  atomic.Int64
	queueSize  atomic.Int64
	sem        chan struct{}
	mu         sync.RWMutex
	metrics    *MetricsCollector
}

// NewAdmissionController creates an admission controller.
// The effective concurrency limit is dynamically updated based on worker count.
func NewAdmissionController(cfg AdmissionConfig, metrics *MetricsCollector) *AdmissionController {
	ac := &AdmissionController{
		cfg:     cfg,
		sem:     make(chan struct{}, cfg.MaxQueueSize+1), // buffered semaphore
		metrics: metrics,
	}
	// Initialize with a reasonable default
	ac.maxActive.Store(int64(cfg.MaxConcurrencyPerWorker))
	return ac
}

// UpdateLimits recalculates the concurrency limit based on current healthy worker count.
func (ac *AdmissionController) UpdateLimits(healthyWorkers int) {
	if healthyWorkers < 1 {
		healthyWorkers = 1
	}
	newMax := int64(healthyWorkers) * int64(ac.cfg.MaxConcurrencyPerWorker)
	ac.maxActive.Store(newMax)
}

// Admit attempts to admit a request. It blocks until the request is admitted or times out.
// Returns a release function that MUST be called when the request completes.
func (ac *AdmissionController) Admit(ctx context.Context) (release func(), err error) {
	maxActive := ac.maxActive.Load()

	// Fast path: if under limit, admit immediately
	current := ac.active.Load()
	if current < maxActive {
		if ac.active.Add(1) <= maxActive {
			return func() { ac.active.Add(-1) }, nil
		}
		// We went over, back off and go through the queue
		ac.active.Add(-1)
	}

	// Check if queue is full (backpressure)
	queueLen := ac.queueSize.Load()
	if queueLen >= int64(ac.cfg.MaxQueueSize) {
		monitoring.Verbose("admission", fmt.Sprintf("queue full: %d/%d, rejecting request", queueLen, ac.cfg.MaxQueueSize))
		return nil, ErrAdmissionQueueFull
	}

	// Enter queue
	ac.queueSize.Add(1)
	if ac.metrics != nil {
		ac.metrics.SetQueueSize(int(ac.queueSize.Load()))
	}
	defer func() {
		ac.queueSize.Add(-1)
		if ac.metrics != nil {
			ac.metrics.SetQueueSize(int(ac.queueSize.Load()))
		}
	}()

	monitoring.Verbose("admission", fmt.Sprintf("request queued (queue=%d, active=%d/%d)", ac.queueSize.Load(), ac.active.Load(), maxActive))

	// Wait with timeout
	timeout := ac.cfg.QueueTimeout
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
			return nil, ErrAdmissionTimeout
		case <-ticker.C:
			maxActive = ac.maxActive.Load()
			if ac.active.Load() < maxActive {
				if ac.active.Add(1) <= maxActive {
					return func() { ac.active.Add(-1) }, nil
				}
				ac.active.Add(-1)
			}
		}
	}
}

// Stats returns current admission controller stats.
func (ac *AdmissionController) Stats() AdmissionStats {
	return AdmissionStats{
		ActiveRequests:   ac.active.Load(),
		MaxConcurrency:   ac.maxActive.Load(),
		QueuedRequests:   ac.queueSize.Load(),
		MaxQueueSize:     int64(ac.cfg.MaxQueueSize),
	}
}

// AdmissionStats holds the admission controller's current state.
type AdmissionStats struct {
	ActiveRequests int64
	MaxConcurrency int64
	QueuedRequests int64
	MaxQueueSize   int64
}

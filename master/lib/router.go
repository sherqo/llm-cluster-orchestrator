/*
 * This file contains the Load Balancer / Router logic for the master server.
 */

package lib

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"master/monitoring"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type AgentInfo struct {
	AgentID string
	Host    string
	Port    int
	AddedAt time.Time
}

var ErrNoWorkersAvailable = errors.New("no workers available")
var ErrWorkerFailed = errors.New("worker failed")

type Strategy string

const (
	StrategyRoundRobin       = "round_robin"
	StrategyLeastConnections = "least_connections"
	StrategyRandom           = "random"
)

// main router struct
type Router struct {
	workers  map[string]*Worker
	workersM sync.RWMutex

	agents  map[string]*AgentInfo
	agentsM sync.RWMutex

	inflight *monitoring.InFlightStore

	strategy  Strategy
	strategyM sync.RWMutex

	rrCounter atomic.Uint64

	// Autoscaler and metrics (set by Serve, read by TUI)
	metrics    *MetricsCollector
	autoscaler *Autoscaler

	// Worker startup time tracking
	startupMu      sync.Mutex
	startupDurations []time.Duration
	startupFailures  int
}

// router methods

func NewRouter() *Router {
	rand.Seed(time.Now().UnixNano())
	return &Router{
		workers:  make(map[string]*Worker),
		agents:   make(map[string]*AgentInfo),
		inflight: monitoring.NewInFlightStore(),
		strategy: StrategyLeastConnections,
	}
}

func (r *Router) RegisterAgent(info AgentInfo) {
	r.agentsM.Lock()
	defer r.agentsM.Unlock()
	r.agents[info.AgentID] = &info
}

func (r *Router) WorkerExists(id string) bool {
	r.workersM.RLock()
	defer r.workersM.RUnlock()
	_, exists := r.workers[id]
	return exists
}

func (r *Router) AddWorkerWithInstance(w *Worker) {
	r.workersM.Lock()
	defer r.workersM.Unlock()
	r.workers[w.id] = w
}

func (r *Router) WorkerCount() int {
	r.workersM.RLock()
	defer r.workersM.RUnlock()
	return len(r.workers)
}

// HealthyWorkerCount returns the number of workers in HEALTHY state.
func (r *Router) HealthyWorkerCount() int {
	r.workersM.RLock()
	defer r.workersM.RUnlock()
	count := 0
	for _, w := range r.workers {
		if w.GetLifecycleState() == StateHealthy {
			count++
		}
	}
	return count
}

// WorkerCountByAgent returns a map of agent_id → number of workers from that agent (#10).
func (r *Router) WorkerCountByAgent() map[string]int {
	r.workersM.RLock()
	defer r.workersM.RUnlock()

	counts := make(map[string]int)
	for _, w := range r.workers {
		agentID := w.AgentID()
		if agentID != "" {
			counts[agentID]++
		}
	}
	return counts
}

func (r *Router) GetAgents() []*AgentInfo {
	r.agentsM.RLock()
	defer r.agentsM.RUnlock()

	agents := make([]*AgentInfo, 0, len(r.agents))
	for _, a := range r.agents {
		agents = append(agents, a)
	}
	return agents
}

// AgentSnapshot is a point-in-time view of an agent for the TUI.
type AgentSnapshot struct {
	AgentID     string
	Host        string
	Port        int
	WorkerCount int
	AddedAt     time.Time
}

// AgentsSnapshot returns a snapshot of all agents with their worker counts.
func (r *Router) AgentsSnapshot() []AgentSnapshot {
	r.agentsM.RLock()
	agents := make([]*AgentInfo, 0, len(r.agents))
	for _, a := range r.agents {
		agents = append(agents, a)
	}
	r.agentsM.RUnlock()

	workerCounts := r.WorkerCountByAgent()

	out := make([]AgentSnapshot, 0, len(agents))
	for _, a := range agents {
		out = append(out, AgentSnapshot{
			AgentID:     a.AgentID,
			Host:        a.Host,
			Port:        a.Port,
			WorkerCount: workerCounts[a.AgentID],
			AddedAt:     a.AddedAt,
		})
	}
	return out
}

// SetMetrics sets the metrics collector reference for TUI access.
func (r *Router) SetMetrics(mc *MetricsCollector) { r.metrics = mc }

// SetAutoscaler sets the autoscaler reference for TUI access.
func (r *Router) SetAutoscaler(as *Autoscaler) { r.autoscaler = as }

// AutoscalerSnapshot returns the current autoscaler state for the TUI.
func (r *Router) AutoscalerSnapshot() AutoscalerSnapshot {
	if r.autoscaler == nil {
		return AutoscalerSnapshot{}
	}
	return r.autoscaler.Snapshot()
}

// MetricsSnapshot returns the current cluster metrics.
func (r *Router) MetricsSnapshot() ClusterMetrics {
	if r.metrics == nil {
		return ClusterMetrics{}
	}
	return r.metrics.Snapshot()
}

// RecordStartupDuration records a successful worker startup time.
func (r *Router) RecordStartupDuration(d time.Duration) {
	r.startupMu.Lock()
	defer r.startupMu.Unlock()
	r.startupDurations = append(r.startupDurations, d)
	if len(r.startupDurations) > 200 {
		r.startupDurations = r.startupDurations[len(r.startupDurations)-200:]
	}
}

// RecordStartupFailure increments the failed startup counter.
func (r *Router) RecordStartupFailure() {
	r.startupMu.Lock()
	defer r.startupMu.Unlock()
	r.startupFailures++
}

// StartupStats returns computed startup time statistics.
type StartupStats struct {
	AvgDuration    time.Duration
	P95Duration    time.Duration
	FailedStartups int
	TotalStartups  int
}

// GetStartupStats computes and returns startup time statistics.
func (r *Router) GetStartupStats() StartupStats {
	r.startupMu.Lock()
	defer r.startupMu.Unlock()

	stats := StartupStats{
		FailedStartups: r.startupFailures,
		TotalStartups:  len(r.startupDurations) + r.startupFailures,
	}

	n := len(r.startupDurations)
	if n == 0 {
		return stats
	}

	var sum float64
	sorted := make([]float64, n)
	for i, d := range r.startupDurations {
		s := d.Seconds()
		sum += s
		sorted[i] = s
	}
	stats.AvgDuration = time.Duration(sum/float64(n)) * time.Second

	// Sort and compute P95
	sortFloat64s(sorted)
	p95Idx := int(float64(n) * 0.95)
	if p95Idx >= n {
		p95Idx = n - 1
	}
	stats.P95Duration = time.Duration(sorted[p95Idx]) * time.Second

	return stats
}

func (r *Router) HandleChat(ctx context.Context, requestID string, req ChatRequest) (ChatResponse, error) {
	var lastErr error

	for attempt := 0; attempt < 3; attempt++ {
		// Boost priority on retry: failed requests get "elite" tier (priority 10000)
		if attempt > 0 {
			if req.Tier != "elite" {
				req.Tier = "elite"
				monitoring.Verbose("router", "request "+requestID+" retry with elite priority")
			}
		}

		worker, err := r.PickWorker(req)
		if err != nil {
			break
		}

		r.AddInFlight(requestID, worker.addr)
		reply, sendErr := worker.Send(ctx, requestID, req)
		r.RemoveInFlight(requestID)

		if sendErr == nil {
			worker.MarkHealthy()
			return ChatResponse{RequestID: requestID, Reply: reply}, nil
		}

		lastErr = sendErr

		// If it's a deadline exceeded, the worker is just overloaded
		if st, ok := status.FromError(sendErr); ok && st.Code() == codes.DeadlineExceeded {
			monitoring.Verbose("router", "worker "+worker.id+" deadline exceeded (overloaded)")
		} else {
			monitoring.Verbose("router", "worker "+worker.id+" returned error: "+sendErr.Error())
		}
	}

	if lastErr != nil {
		return ChatResponse{}, fmt.Errorf("%w: %w", ErrWorkerFailed, lastErr)
	}

	return ChatResponse{}, ErrNoWorkersAvailable
}

func (r *Router) AddInFlight(requestID, workerAddr string) {
	r.inflight.Add(requestID, workerAddr)
}

func (r *Router) RemoveInFlight(requestID string) {
	r.inflight.Remove(requestID)
}

func (r *Router) InFlightCount() int {
	return r.inflight.Count()
}

func (r *Router) InFlightSnapshot() []monitoring.InFlight {
	return r.inflight.GetAll()
}

func (r *Router) InFlightRecent(limit int) []monitoring.CompletedFlight {
	return r.inflight.Recent(limit)
}

// InFlightForWorker returns the number of inflight requests for a specific worker address.
func (r *Router) InFlightForWorker(workerAddr string) int {
	all := r.inflight.GetAll()
	count := 0
	for _, f := range all {
		if f.Worker == workerAddr {
			count++
		}
	}
	return count
}

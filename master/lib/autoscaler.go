/*
 * Production-grade autoscaler engine for the LLM cluster orchestrator.
 *
 * Solves:
 *   #1  No Cooldown       — scale-up and scale-down cooldowns with timestamp tracking
 *   #3  No Scale Down     — low-load detection with sustained low utilization and min workers
 *   #4  No Hysteresis     — separate scale-up / scale-down thresholds
 *   #8  Fixed Scaling     — proportional scaling: calculates exact workers needed
 *   #9  No Scaling Lock   — mutex ensures single active scaling operation
 *   #10 Naive Agent       — resource-aware agent selection (least workers first)
 *   #11 No Failure Recov  — retries with exponential backoff + agent health tracking
 *   #14 No Predictive     — trend-based prewarming when traffic is growing
 */

package lib

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"master/monitoring"
)

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

// AutoscalerConfig contains all tunable knobs for the autoscaler.
type AutoscalerConfig struct {
	// Cooldowns
	ScaleUpCooldown   time.Duration // minimum time between consecutive scale-up actions
	ScaleDownCooldown time.Duration // minimum time between consecutive scale-down actions

	// Hysteresis thresholds (worker utilization 0.0–1.0)
	ScaleUpThreshold   float64 // utilization above this triggers scale-up
	ScaleDownThreshold float64 // utilization below this triggers scale-down

	// Sustained-duration checks: how long the condition must persist before acting
	ScaleUpSustained   time.Duration
	ScaleDownSustained time.Duration

	// Proportional scaling
	TargetUtilization float64 // desired utilization (e.g. 0.7 = 70%)
	MaxScaleUpStep    int     // maximum workers to add in one operation
	MaxScaleDownStep  int     // maximum workers to remove in one operation

	// Bounds
	MinWorkers int // never scale below this
	MaxWorkers int // never scale above this (0 = no limit)

	// Predictive scaling
	RPSTrendThreshold float64 // slope above this triggers prewarming
	PrewarmWorkers    int     // how many extra workers to prewarm

	// Error rate threshold: if error rate exceeds this, block scale-down
	ErrorRateCeiling float64

	// P95 latency ceiling: if P95 latency exceeds this (seconds), trigger scale-up
	P95LatencyCeiling float64

	// Tick interval: how often the autoscaler evaluates
	TickInterval time.Duration

	// Failure recovery
	SpawnMaxRetries     int           // max retries for worker spawning
	SpawnRetryBackoff   time.Duration // initial backoff duration
	AgentFailurePenalty time.Duration // how long to penalize a failing agent

	// Async callback: master's base URL the agent will POST to when the worker is ready.
	// If empty, defaults to http://localhost:8080.
	CallbackBaseURL string
}

// DefaultAutoscalerConfig returns sensible production defaults.
func DefaultAutoscalerConfig() AutoscalerConfig {
	return AutoscalerConfig{
		ScaleUpCooldown:   30 * time.Second,
		ScaleDownCooldown: 60 * time.Second,

		ScaleUpThreshold:   0.75,
		ScaleDownThreshold: 0.25,

		ScaleUpSustained:   10 * time.Second,
		ScaleDownSustained: 30 * time.Second,

		TargetUtilization: 0.65,
		MaxScaleUpStep:    5,
		MaxScaleDownStep:  2,

		MinWorkers: 1,
		MaxWorkers: 50,

		RPSTrendThreshold: 2.0,
		PrewarmWorkers:    1,

		ErrorRateCeiling:  0.1,
		P95LatencyCeiling: 5.0,

		TickInterval: 5 * time.Second,

		SpawnMaxRetries:     3,
		SpawnRetryBackoff:   2 * time.Second,
		AgentFailurePenalty: 30 * time.Second,

		CallbackBaseURL: "http://localhost:8080",
	}
}

// ---------------------------------------------------------------------------
// Agent health tracking (for failure recovery, issue #11)
// ---------------------------------------------------------------------------

type agentHealth struct {
	consecutiveFailures int
	lastFailure         time.Time
	penalizedUntil      time.Time
}

// ---------------------------------------------------------------------------
// Autoscaler
// ---------------------------------------------------------------------------

// Autoscaler is the main autoscaling engine.
type Autoscaler struct {
	cfg     AutoscalerConfig
	router  *Router
	metrics *MetricsCollector

	// Scaling lock (#9)
	scalingMu    sync.Mutex
	scalingInOp  atomic.Bool
	lastScaleUp  time.Time
	lastScaleDown time.Time

	// Sustained condition tracking (#5)
	highLoadSince time.Time
	lowLoadSince  time.Time
	highLoadSet   bool
	lowLoadSet    bool

	// Agent health tracking (#11)
	agentHealthMu sync.Mutex
	agentHealth   map[string]*agentHealth
}

// NewAutoscaler creates a new autoscaler with the given configuration.
func NewAutoscaler(cfg AutoscalerConfig, router *Router, metrics *MetricsCollector) *Autoscaler {
	return &Autoscaler{
		cfg:         cfg,
		router:      router,
		metrics:     metrics,
		agentHealth: make(map[string]*agentHealth),
	}
}

// Run starts the autoscaler loop. Blocks forever — run in a goroutine.
func (as *Autoscaler) Run() {
	monitoring.Verbose("autoscaler", fmt.Sprintf(
		"started: tick=%s, scaleUp=%.0f%%, scaleDown=%.0f%%, target=%.0f%%, min=%d, max=%d",
		as.cfg.TickInterval, as.cfg.ScaleUpThreshold*100, as.cfg.ScaleDownThreshold*100,
		as.cfg.TargetUtilization*100, as.cfg.MinWorkers, as.cfg.MaxWorkers,
	))

	for {
		time.Sleep(as.cfg.TickInterval)
		as.tick()
	}
}

// tick is a single evaluation cycle.
func (as *Autoscaler) tick() {
	// Collect fresh metrics
	m := as.metrics.Collect(as.router)

	// If no workers exist and we have agents, bootstrap minimum workers.
	// Respect cooldown to prevent spamming when agents are unreachable.
	if m.HealthyWorkers == 0 && m.TotalWorkers == 0 {
		now := time.Now()
		if !as.lastScaleUp.IsZero() && now.Sub(as.lastScaleUp) < as.cfg.ScaleUpCooldown {
			return // still in cooldown from last attempt
		}
		agents := as.router.GetAgents()
		if len(agents) > 0 && as.cfg.MinWorkers > 0 {
			monitoring.Verbose("autoscaler", "no workers, bootstrapping minimum")
			as.scaleUp(as.cfg.MinWorkers, m)
		}
		return
	}

	// Evaluate scale-up conditions
	scaleUpNeeded, scaleUpCount := as.evaluateScaleUp(m)

	// Evaluate scale-down conditions
	scaleDownNeeded, scaleDownCount := as.evaluateScaleDown(m)

	// Predictive: if traffic trend is strongly positive, prewarm
	if !scaleUpNeeded && m.RPSTrend > as.cfg.RPSTrendThreshold {
		monitoring.Verbose("autoscaler", fmt.Sprintf("predictive: RPS trend=%.2f, prewarming %d workers", m.RPSTrend, as.cfg.PrewarmWorkers))
		scaleUpNeeded = true
		scaleUpCount = as.cfg.PrewarmWorkers
	}

	// Act — only one direction per tick, scale-up takes priority
	if scaleUpNeeded {
		as.scaleUp(scaleUpCount, m)
	} else if scaleDownNeeded {
		as.scaleDown(scaleDownCount, m)
	}
}

// ---------------------------------------------------------------------------
// Scale-up evaluation (#1, #4, #5, #8, #14)
// ---------------------------------------------------------------------------

func (as *Autoscaler) evaluateScaleUp(m ClusterMetrics) (bool, int) {
	now := time.Now()

	// Check cooldown (#1)
	if !as.lastScaleUp.IsZero() && now.Sub(as.lastScaleUp) < as.cfg.ScaleUpCooldown {
		return false, 0
	}

	// Check max workers bound
	if as.cfg.MaxWorkers > 0 && m.TotalWorkers >= as.cfg.MaxWorkers {
		return false, 0
	}

	highLoad := false

	// Condition 1: utilization above threshold (#4 hysteresis — separate threshold)
	if m.WorkerUtilization > as.cfg.ScaleUpThreshold {
		highLoad = true
	}

	// Condition 2: P95 latency exceeds ceiling
	if m.P95Latency > as.cfg.P95LatencyCeiling && m.HealthyWorkers > 0 {
		highLoad = true
	}

	// Condition 3: queue is building up
	if m.QueueDepth > float64(m.HealthyWorkers) {
		highLoad = true
	}

	if !highLoad {
		as.highLoadSet = false
		return false, 0
	}

	// Sustained-duration check (#5)
	if !as.highLoadSet {
		as.highLoadSince = now
		as.highLoadSet = true
	}
	if now.Sub(as.highLoadSince) < as.cfg.ScaleUpSustained {
		return false, 0
	}

	// Proportional scaling: calculate how many workers we need (#8)
	needed := as.calculateScaleUpCount(m)
	if needed <= 0 {
		return false, 0
	}
	if needed > as.cfg.MaxScaleUpStep {
		needed = as.cfg.MaxScaleUpStep
	}
	if as.cfg.MaxWorkers > 0 && m.TotalWorkers+needed > as.cfg.MaxWorkers {
		needed = as.cfg.MaxWorkers - m.TotalWorkers
	}
	if needed <= 0 {
		return false, 0
	}

	return true, needed
}

// calculateScaleUpCount uses target utilization to compute desired workers. (#8)
func (as *Autoscaler) calculateScaleUpCount(m ClusterMetrics) int {
	if m.HealthyWorkers == 0 {
		return as.cfg.MinWorkers
	}

	// desired = ceil(current_load / (target_utilization * max_concurrency))
	currentLoad := m.WorkerUtilization * float64(m.HealthyWorkers)
	desiredWorkers := math.Ceil(currentLoad / as.cfg.TargetUtilization)
	delta := int(desiredWorkers) - m.HealthyWorkers
	if delta < 1 {
		delta = 1 // at least 1 if we're here
	}
	return delta
}

// ---------------------------------------------------------------------------
// Scale-down evaluation (#1, #3, #4, #5)
// ---------------------------------------------------------------------------

func (as *Autoscaler) evaluateScaleDown(m ClusterMetrics) (bool, int) {
	now := time.Now()

	// Check cooldown (#1)
	if !as.lastScaleDown.IsZero() && now.Sub(as.lastScaleDown) < as.cfg.ScaleDownCooldown {
		return false, 0
	}

	// Never go below minimum (#3)
	if m.HealthyWorkers <= as.cfg.MinWorkers {
		return false, 0
	}

	// Don't scale down if error rate is high — something is wrong, don't remove capacity
	if m.ErrorRate > as.cfg.ErrorRateCeiling {
		return false, 0
	}

	lowLoad := false

	// Condition: utilization below scale-down threshold (#4 hysteresis)
	if m.WorkerUtilization < as.cfg.ScaleDownThreshold && m.QueueDepth < 1 {
		lowLoad = true
	}

	if !lowLoad {
		as.lowLoadSet = false
		return false, 0
	}

	// Sustained-duration check (#5)
	if !as.lowLoadSet {
		as.lowLoadSince = now
		as.lowLoadSet = true
	}
	if now.Sub(as.lowLoadSince) < as.cfg.ScaleDownSustained {
		return false, 0
	}

	// Proportional: how many can we safely remove?
	excessWorkers := as.calculateScaleDownCount(m)
	if excessWorkers <= 0 {
		return false, 0
	}
	if excessWorkers > as.cfg.MaxScaleDownStep {
		excessWorkers = as.cfg.MaxScaleDownStep
	}
	// Never go below minimum
	if m.HealthyWorkers-excessWorkers < as.cfg.MinWorkers {
		excessWorkers = m.HealthyWorkers - as.cfg.MinWorkers
	}
	if excessWorkers <= 0 {
		return false, 0
	}

	return true, excessWorkers
}

func (as *Autoscaler) calculateScaleDownCount(m ClusterMetrics) int {
	if m.HealthyWorkers <= as.cfg.MinWorkers {
		return 0
	}

	// desired = ceil(current_load / target_utilization)
	currentLoad := m.WorkerUtilization * float64(m.HealthyWorkers)
	desiredWorkers := int(math.Ceil(currentLoad / as.cfg.TargetUtilization))
	if desiredWorkers < as.cfg.MinWorkers {
		desiredWorkers = as.cfg.MinWorkers
	}
	excess := m.HealthyWorkers - desiredWorkers
	if excess < 0 {
		excess = 0
	}
	return excess
}

// ---------------------------------------------------------------------------
// Scale-up execution (#9, #10, #11)
// ---------------------------------------------------------------------------

func (as *Autoscaler) scaleUp(count int, m ClusterMetrics) {
	// Scaling lock (#9)
	if !as.scalingInOp.CompareAndSwap(false, true) {
		monitoring.Verbose("autoscaler", "scale-up skipped: another scaling operation in progress")
		return
	}
	defer as.scalingInOp.Store(false)

	as.scalingMu.Lock()
	defer as.scalingMu.Unlock()

	// Always record the attempt time so cooldown is enforced even on failure.
	// This prevents the bootstrap loop from spamming every tick when agents are down.
	defer func() { as.lastScaleUp = time.Now() }()

	monitoring.Verbose("autoscaler", fmt.Sprintf(
		"scale-up: adding %d workers (util=%.2f, rps=%.1f, p95=%.2fs, queue=%.1f, healthy=%d)",
		count, m.WorkerUtilization, m.RequestsPerSec, m.P95Latency, m.QueueDepth, m.HealthyWorkers,
	))

	agents := as.router.GetAgents()
	if len(agents) == 0 {
		monitoring.Verbose("autoscaler", "scale-up failed: no agents registered")
		return
	}

	spawned := 0
	for i := 0; i < count; i++ {
		// Pick best agent (#10 — least workers, skip penalized)
		agent := as.pickBestAgent(agents)
		if agent == nil {
			monitoring.Verbose("autoscaler", fmt.Sprintf(
				"no eligible agents available (all %d agents penalized), will retry after cooldown",
				len(agents),
			))
			break
		}

		err := as.spawnWorkerWithRetry(agent)
		if err != nil {
			monitoring.Verbose("autoscaler", fmt.Sprintf("failed to spawn on agent %s: %v", agent.AgentID, err))
			as.recordAgentFailure(agent.AgentID)
		} else {
			spawned++
			as.resetAgentFailure(agent.AgentID)
		}
	}

	if spawned > 0 {
		as.highLoadSet = false
		monitoring.Verbose("autoscaler", fmt.Sprintf("scale-up complete: spawned %d/%d workers", spawned, count))
	} else {
		monitoring.Verbose("autoscaler", fmt.Sprintf("scale-up failed: 0/%d workers spawned, cooldown %s", count, as.cfg.ScaleUpCooldown))
	}
}

// ---------------------------------------------------------------------------
// Scale-down execution (#3, #7, #9)
// ---------------------------------------------------------------------------

func (as *Autoscaler) scaleDown(count int, m ClusterMetrics) {
	// Scaling lock (#9)
	if !as.scalingInOp.CompareAndSwap(false, true) {
		monitoring.Verbose("autoscaler", "scale-down skipped: another scaling operation in progress")
		return
	}
	defer as.scalingInOp.Store(false)

	as.scalingMu.Lock()
	defer as.scalingMu.Unlock()

	monitoring.Verbose("autoscaler", fmt.Sprintf(
		"scale-down: removing %d workers (util=%.2f, rps=%.1f, queue=%.1f, healthy=%d)",
		count, m.WorkerUtilization, m.RequestsPerSec, m.QueueDepth, m.HealthyWorkers,
	))

	// Find workers to drain — pick those with the least active requests
	candidates := as.selectDrainCandidates(count)

	drained := 0
	for _, w := range candidates {
		monitoring.Verbose("autoscaler", "draining worker "+w.ID()+" for scale-down")
		go as.drainAndRemoveWorker(w) // safe drain in background (#7)
		drained++
	}

	if drained > 0 {
		as.lastScaleDown = time.Now()
		as.lowLoadSet = false
		monitoring.Verbose("autoscaler", fmt.Sprintf("scale-down initiated: draining %d workers", drained))
	}
}

// selectDrainCandidates picks the N least-busy healthy workers for draining.
func (as *Autoscaler) selectDrainCandidates(count int) []*Worker {
	as.router.workersM.RLock()
	defer as.router.workersM.RUnlock()

	type workerLoad struct {
		w      *Worker
		active int64
	}

	candidates := make([]workerLoad, 0, len(as.router.workers))
	for _, w := range as.router.workers {
		if w.GetLifecycleState() == StateHealthy {
			candidates = append(candidates, workerLoad{w: w, active: w.ActiveRequests()})
		}
	}

	// Sort by active requests ascending (least busy first)
	for i := 1; i < len(candidates); i++ {
		key := candidates[i]
		j := i - 1
		for j >= 0 && candidates[j].active > key.active {
			candidates[j+1] = candidates[j]
			j--
		}
		candidates[j+1] = key
	}

	result := make([]*Worker, 0, count)
	for i := 0; i < count && i < len(candidates); i++ {
		result = append(result, candidates[i].w)
	}
	return result
}

// drainAndRemoveWorker implements safe scale-down (#7):
// 1. Mark worker as DRAINING (stops new routing)
// 2. Wait for active requests to finish
// 3. Mark as STOPPING, close connection
// 4. Remove from router
func (as *Autoscaler) drainAndRemoveWorker(w *Worker) {
	workerID := w.ID()

	// Step 1: Mark as draining — no new requests will be routed
	w.SetLifecycleState(StateDraining)
	monitoring.Verbose("autoscaler", "worker "+workerID+" now draining")

	// Step 2: Wait for active requests to complete (with timeout)
	drainTimeout := 120 * time.Second
	deadline := time.Now().Add(drainTimeout)
	for w.ActiveRequests() > 0 && time.Now().Before(deadline) {
		time.Sleep(1 * time.Second)
	}

	if w.ActiveRequests() > 0 {
		monitoring.Verbose("autoscaler", fmt.Sprintf("worker %s drain timeout with %d active requests", workerID, w.ActiveRequests()))
	}

	// Step 3: Mark stopping and close connection
	w.SetLifecycleState(StateStopping)
	monitoring.Verbose("autoscaler", "worker "+workerID+" stopping")

	if err := w.Close(); err != nil {
		monitoring.Verbose("autoscaler", "worker "+workerID+" close error: "+err.Error())
	}

	// Step 4: Mark dead and remove from router
	w.SetLifecycleState(StateDead)
	if err := as.router.RemoveWorker(workerID); err != nil {
		monitoring.Verbose("autoscaler", "worker "+workerID+" removal error: "+err.Error())
	}

	monitoring.Verbose("autoscaler", "worker "+workerID+" fully removed")
}

// ---------------------------------------------------------------------------
// Agent selection (#10) — resource-aware
// ---------------------------------------------------------------------------

// pickBestAgent selects the most suitable agent for spawning a new worker.
// Uses least-workers-first strategy, skipping penalized agents.
func (as *Autoscaler) pickBestAgent(agents []*AgentInfo) *AgentInfo {
	as.agentHealthMu.Lock()
	defer as.agentHealthMu.Unlock()

	now := time.Now()

	// Count workers per agent
	workerCounts := as.router.WorkerCountByAgent()

	var best *AgentInfo
	bestScore := int(math.MaxInt32)

	for _, agent := range agents {
		// Skip penalized agents (#11)
		if h, ok := as.agentHealth[agent.AgentID]; ok {
			if now.Before(h.penalizedUntil) {
				continue
			}
		}

		count := workerCounts[agent.AgentID]
		if count < bestScore {
			bestScore = count
			best = agent
		}
	}

	return best
}

// ---------------------------------------------------------------------------
// Worker spawning with retry (#11)
// ---------------------------------------------------------------------------

func (as *Autoscaler) spawnWorkerWithRetry(agent *AgentInfo) error {
	agentAddr := fmt.Sprintf("http://%s:%d", agent.Host, agent.Port)
	backoff := as.cfg.SpawnRetryBackoff

	var lastErr error
	for attempt := 1; attempt <= as.cfg.SpawnMaxRetries; attempt++ {
		err := as.spawnWorkerOnAgent(agentAddr, agent.AgentID)
		if err == nil {
			return nil
		}
		lastErr = err
		monitoring.Verbose("autoscaler", fmt.Sprintf(
			"spawn attempt %d/%d on agent %s failed: %v (backoff %s)",
			attempt, as.cfg.SpawnMaxRetries, agent.AgentID, err, backoff,
		))

		if attempt < as.cfg.SpawnMaxRetries {
			time.Sleep(backoff)
			backoff *= 2 // exponential backoff
		}
	}

	return fmt.Errorf("all %d spawn attempts failed on agent %s: %w",
		as.cfg.SpawnMaxRetries, agent.AgentID, lastErr)
}

func (as *Autoscaler) spawnWorkerOnAgent(agentAddr, agentID string) error {
	// Fire-and-forget: send the create request to the agent with a callback URL.
	// The agent starts the container async, polls until gRPC port is open,
	// then POSTs to /workers/ready on the master. No blocking wait here.
	callbackURL := as.cfg.CallbackBaseURL + "/workers/ready"

	body, err := json.Marshal(map[string]string{
		"callback_url": callbackURL,
		"agent_id":     agentID,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal spawn request: %w", err)
	}

	// 10s is enough for the HTTP delivery ack; agent responds 202 Accepted immediately
	client := http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(agentAddr+"/workers/create", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	// Accept both 200 OK (legacy) and 202 Accepted (async path)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("agent returned status %d", resp.StatusCode)
	}

	monitoring.Verbose("autoscaler", fmt.Sprintf(
		"spawn request accepted by agent %s — waiting for /workers/ready callback",
		agentID,
	))
	return nil
}

// ---------------------------------------------------------------------------
// Agent health tracking (#11)
// ---------------------------------------------------------------------------

func (as *Autoscaler) recordAgentFailure(agentID string) {
	as.agentHealthMu.Lock()
	defer as.agentHealthMu.Unlock()

	h, ok := as.agentHealth[agentID]
	if !ok {
		h = &agentHealth{}
		as.agentHealth[agentID] = h
	}

	h.consecutiveFailures++
	h.lastFailure = time.Now()

	// Apply escalating penalty: base * 2^(failures-1), capped at 5 minutes.
	// First failure = 30s, second = 60s, third = 120s, etc.
	penalty := as.cfg.AgentFailurePenalty * time.Duration(1<<uint(h.consecutiveFailures-1))
	if penalty > 5*time.Minute {
		penalty = 5 * time.Minute
	}
	h.penalizedUntil = time.Now().Add(penalty)

	monitoring.Verbose("autoscaler", fmt.Sprintf(
		"agent %s: %d consecutive failures, penalized for %s",
		agentID, h.consecutiveFailures, penalty,
	))
}

func (as *Autoscaler) resetAgentFailure(agentID string) {
	as.agentHealthMu.Lock()
	defer as.agentHealthMu.Unlock()

	if h, ok := as.agentHealth[agentID]; ok {
		h.consecutiveFailures = 0
		h.penalizedUntil = time.Time{}
	}
}

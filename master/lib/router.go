/*
* This file contains the Load Balancer / Router logic for the master server.
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
}

// router methods

func NewRouter() *Router {
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

func (r *Router) GetAgents() []*AgentInfo {
	r.agentsM.RLock()
	defer r.agentsM.RUnlock()

	agents := make([]*AgentInfo, 0, len(r.agents))
	for _, a := range r.agents {
		agents = append(agents, a)
	}
	return agents
}

func (r *Router) HandleChat(ctx context.Context, requestID string, req ChatRequest) (ChatResponse, error) {
	var lastErr error

	for attemptsLeft := 3; attemptsLeft > 0; attemptsLeft-- {
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

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
)

var ErrNoWorkersAvailable = errors.New("no workers available")
var ErrWorkerFailed = errors.New("worker failed")

type Strategy string

const (
	StrategyRoundRobin        = "round_robin"
	StrategyLeastConnections  = "least_connections"
	StrategyWeightedLeastLoad = "weighted_least_load"
)

// main router struct
type Router struct {
	workers  map[string]*Worker
	workersM sync.RWMutex

	inFlight  map[string]InFlight
	inFlightM sync.RWMutex

	strategy  Strategy
	strategyM sync.RWMutex

	rrCounter atomic.Uint64
}

// router methods

func NewRouter() *Router {
	return &Router{
		workers:  make(map[string]*Worker),
		inFlight: make(map[string]InFlight),
		strategy: StrategyLeastConnections,
	}
}

func (r *Router) AddWorker(addr string) {
	id := "worker-" + addr

	r.workersM.Lock()
	defer r.workersM.Unlock()

	w, err := NewWorker(id, addr, 1)
	if err != nil {
		return
	}
	r.workers[id] = w
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

		worker.MarkSuspected()
		lastErr = sendErr
	}

	if lastErr != nil {
		return ChatResponse{}, fmt.Errorf("%w: %w", ErrWorkerFailed, lastErr)
	}

	return ChatResponse{}, ErrNoWorkersAvailable
}

func (r *Router) StartCircuitRecoveryLoop() {
	ticker := time.NewTicker(1 * time.Second)

	go func() {
		for range ticker.C {
			r.workersM.RLock()
			workers := make([]*Worker, 0, len(r.workers))
			for _, worker := range r.workers {
				workers = append(workers, worker)
			}
			r.workersM.RUnlock()

			for _, worker := range workers {
				worker.maybeHalfOpen()
			}
		}
	}()
}

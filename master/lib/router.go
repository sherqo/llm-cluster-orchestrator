/*
* This file contains the Load Balancer / Router logic for the master server.
 */

package lib

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

var ErrNoWorkersAvailable = errors.New("no workers available")
var ErrWorkerFailed = errors.New("worker failed")

// main router struct 
type Router struct {
	workers map[string]*Worker // map of workers
	workersM sync.RWMutex

	inFlight  map[string]InFlight
	inFlightM sync.RWMutex
}

// struct to track in-flight requests (is not needed for the application logic, but can be useful for monitoring and debugging)
type InFlight struct {
	RequestID string
	Worker    string
	StartedAt time.Time
}

// router methods

func NewRouter() *Router {
	return &Router{
		workers:  make(map[string]*Worker),
		inFlight: make(map[string]InFlight),
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

func (r *Router) Pick(req ChatRequest) (*Worker, error) {
	r.workersM.RLock()
	defer r.workersM.RUnlock()

	var best *Worker
	var bestScore float64

	for _, worker := range r.workers {
		if !worker.isRoutable() {
			continue
		}

		score := worker.loadScore()
		if best == nil || score < bestScore {
			best = worker
			bestScore = score
		}
	}

	if best == nil {
		return nil, ErrNoWorkersAvailable
	}

	return best, nil
}

func (r *Router) HandleChat(ctx context.Context, requestID string, req ChatRequest) (ChatResponse, error) {
	worker, err := r.Pick(req)
	if err != nil {
		return ChatResponse{}, err
	}

	r.AddInFlight(requestID, worker.addr)
	defer r.RemoveInFlight(requestID)

	reply, err := worker.Send(ctx, requestID, req)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("%w: %v", ErrWorkerFailed, err)
	}

	return ChatResponse{
		RequestID: requestID,
		Reply:     reply,
	}, nil
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

func (r *Router) AddInFlight(requestID string, workerAddr string) {
	r.inFlightM.Lock()
	defer r.inFlightM.Unlock()

	r.inFlight[requestID] = InFlight{
		RequestID: requestID,
		Worker:    workerAddr,
		StartedAt: time.Now(),
	}
}

func (r *Router) RemoveInFlight(requestID string) {
	r.inFlightM.Lock()
	defer r.inFlightM.Unlock()

	delete(r.inFlight, requestID)
}

func (r *Router) InFlightCount() int {
	r.inFlightM.RLock()
	defer r.inFlightM.RUnlock()

	return len(r.inFlight)
}

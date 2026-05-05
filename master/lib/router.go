package lib

import (
	"errors"
	"sync"
	"time"
)

type Router struct {
	workers map[string]*Worker
	mu      sync.RWMutex

	inFlight  map[string]InFlight
	inFlightM sync.RWMutex
}

type InFlight struct {
	RequestID string
	Worker    string
	StartedAt time.Time
}

func NewRouter() *Router {
	return &Router{
		workers:  make(map[string]*Worker),
		inFlight: make(map[string]InFlight),
	}
}

func (r *Router) AddWorker(addr string) {
	id := "worker-" + addr

	r.mu.Lock()
	defer r.mu.Unlock()

	w, err := NewWorker(id, addr, 1)
	if err != nil {
		return
	}
	r.workers[id] = w
}

func (r *Router) Pick(req ChatRequest) (*Worker, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

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
		return nil, errors.New("no routable workers available")
	}

	return best, nil
}

func (r *Router) StartCircuitRecoveryLoop() {
	ticker := time.NewTicker(1 * time.Second)

	go func() {
		for range ticker.C {
			r.mu.RLock()
			workers := make([]*Worker, 0, len(r.workers))
			for _, worker := range r.workers {
				workers = append(workers, worker)
			}
			r.mu.RUnlock()

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

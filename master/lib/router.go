package lib

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

// Router holds all known workers and picks one per request.
// Currently uses round-robin. Swap Pick() logic for anything else later.
type Router struct {
	workers   []*Worker
	counter   atomic.Uint64
	inFlight  map[string]InFlight
	inFlightM sync.RWMutex
}

type InFlight struct {
	RequestID string
	Worker    string
	StartedAt time.Time
}

func NewRouter() *Router {
	return &Router{inFlight: make(map[string]InFlight)}
}

func (r *Router) AddWorker(addr string) {
	r.workers = append(r.workers, NewWorker(addr))
}

func (r *Router) Pick(req ChatRequest) (*Worker, error) {
	if len(r.workers) == 0 {
		return nil, errors.New("no workers registered")
	}

	// round-robin
	idx := r.counter.Add(1) % uint64(len(r.workers))
	return r.workers[idx], nil
}

func (r *Router) AddInFlight(requestID string, workerAddr string) {
	r.inFlightM.Lock()
	defer r.inFlightM.Unlock()
	r.inFlight[requestID] = InFlight{RequestID: requestID, Worker: workerAddr, StartedAt: time.Now()}
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

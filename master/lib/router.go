package lib

import (
	"errors"
	"sync/atomic"
)

// Router holds all known workers and picks one per request.
// Currently uses round-robin. Swap Pick() logic for anything else later.
type Router struct {
	workers []*Worker
	counter atomic.Uint64
}

func NewRouter() *Router {
	return &Router{}
}

func (r *Router) AddWorker(addr string) {
	r.workers = append(r.workers, NewWorker(addr))
}

func (r *Router) Pick(req ChatRequest) (*Worker, error) {
	if len(r.workers) == 0 {
		return nil, errors.New("no workers registered")
	}

	// round-robin: atomic counter so it's safe across goroutines
	idx := r.counter.Add(1) % uint64(len(r.workers))
	return r.workers[idx], nil
}
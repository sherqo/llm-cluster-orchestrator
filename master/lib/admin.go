package lib

import (
	"errors"
	"fmt"
	"sort"
)

var ErrWorkerNotFound = errors.New("worker not found")

type WorkerSnapshot struct {
	ID             string
	Addr           string
	Weight         float64
	Status         WorkerStatus
	CircuitState   CircuitState
	Failures       int64
	Successes      int64
	ActiveRequests int64
	LoadScore      float64
}

func (r *Router) AddWorkerWithWeight(addr string, weight float64) error {
	id := "worker-" + addr

	r.workersM.Lock()
	defer r.workersM.Unlock()

	if _, exists := r.workers[id]; exists {
		return fmt.Errorf("worker already exists: %s", id)
	}

	w, err := NewWorker(id, addr, weight)
	if err != nil {
		return err
	}
	r.workers[id] = w
	return nil
}

func (r *Router) RemoveWorker(id string) error {
	r.workersM.Lock()
	defer r.workersM.Unlock()

	w, ok := r.workers[id]
	if !ok {
		return ErrWorkerNotFound
	}

	delete(r.workers, id)
	return w.Close()
}

func (r *Router) DrainWorker(id string) error {
	r.workersM.RLock()
	w, ok := r.workers[id]
	r.workersM.RUnlock()
	if !ok {
		return ErrWorkerNotFound
	}
	w.SetDraining()
	return nil
}

func (r *Router) WorkersSnapshot() []WorkerSnapshot {
	r.workersM.RLock()
	defer r.workersM.RUnlock()

	out := make([]WorkerSnapshot, 0, len(r.workers))
	for _, w := range r.workers {
		out = append(out, w.Snapshot())
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})

	return out
}

func (r *Router) Strategy() Strategy {
	r.strategyM.RLock()
	defer r.strategyM.RUnlock()
	return r.strategy
}

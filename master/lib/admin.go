package lib

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"time"

	"master/monitoring"
)

var ErrWorkerNotFound = errors.New("worker not found")

type WorkerSnapshot struct {
	ID             string
	Addr           string
	Status         WorkerStatus   // legacy status for backward compat
	Lifecycle      LifecycleState // full lifecycle state (#6)
	Failures       int64
	Successes      int64
	ActiveRequests int64
	AgentID        string // which agent spawned this worker (#10)
}

func (r *Router) AddWorker(addr string) error {
	id := "worker-" + addr

	r.workersM.Lock()
	defer r.workersM.Unlock()

	if _, exists := r.workers[id]; exists {
		return fmt.Errorf("worker already exists: %s", id)
	}

	w, err := NewWorker(id, addr)
	if err != nil {
		return err
	}
	r.workers[id] = w
	return nil
}

func (r *Router) RemoveWorker(id string) error {
	r.workersM.Lock()
	w, ok := r.workers[id]
	if !ok {
		r.workersM.Unlock()
		return ErrWorkerNotFound
	}
	delete(r.workers, id)
	r.workersM.Unlock()

	var closeErr error
	if w != nil {
		closeErr = w.Close()
		if agentID := w.AgentID(); agentID != "" {
			// Run asynchronously to prevent blocking the caller (like the TUI thread)
			go r.destroyWorkerOnAgent(w.id, agentID)
		}
	}

	return closeErr
}

// destroyWorkerOnAgent sends a DELETE /workers/destroy to the agent hosting this worker.
// Best-effort: if the agent is unreachable, the error is logged but not returned.
func (r *Router) destroyWorkerOnAgent(workerID, agentID string) {
	r.agentsM.RLock()
	agent, ok := r.agents[agentID]
	r.agentsM.RUnlock()
	if !ok {
		monitoring.Verbose("admin", "agent "+agentID+" not found for destroying worker "+workerID)
		return
	}

	url := fmt.Sprintf("http://%s:%d/workers/destroy?worker_id=%s", agent.Host, agent.Port, workerID)
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		monitoring.Verbose("admin", "failed to create destroy request for "+workerID+": "+err.Error())
		return
	}

	client := http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		monitoring.Verbose("admin", "failed to destroy worker "+workerID+" on agent "+agentID+": "+err.Error())
		return
	}
	resp.Body.Close()
	monitoring.Verbose("admin", "destroyed worker "+workerID+" on agent "+agentID)
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

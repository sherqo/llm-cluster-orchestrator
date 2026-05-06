package monitoring

import (
	"sync"
	"time"
)

type InFlight struct {
	RequestID string
	Worker    string
	StartedAt time.Time
}

type InFlightStore struct {
	mu       sync.RWMutex
	requests map[string]InFlight
}

func NewInFlightStore() *InFlightStore {
	return &InFlightStore{requests: make(map[string]InFlight)}
}

func (t *InFlightStore) Add(requestID, workerAddr string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.requests[requestID] = InFlight{
		RequestID: requestID,
		Worker:    workerAddr,
		StartedAt: time.Now(),
	}
}

func (t *InFlightStore) Remove(requestID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.requests, requestID)
}

func (t *InFlightStore) Count() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.requests)
}

func (t *InFlightStore) GetAll() []InFlight {
	t.mu.RLock()
	defer t.mu.RUnlock()

	out := make([]InFlight, 0, len(t.requests))
	for _, req := range t.requests {
		out = append(out, req)
	}
	return out
}

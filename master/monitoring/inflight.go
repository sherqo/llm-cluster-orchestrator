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

type CompletedFlight struct {
	RequestID string
	Worker    string
	StartedAt time.Time
	EndedAt   time.Time
	Duration  time.Duration
}

type InFlightStore struct {
	mu        sync.RWMutex
	requests  map[string]InFlight
	completed []CompletedFlight
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
	flight, ok := t.requests[requestID]
	if ok {
		endedAt := time.Now()
		t.completed = append(t.completed, CompletedFlight{
			RequestID: flight.RequestID,
			Worker:    flight.Worker,
			StartedAt: flight.StartedAt,
			EndedAt:   endedAt,
			Duration:  endedAt.Sub(flight.StartedAt),
		})
		if len(t.completed) > 2000 {
			t.completed = append([]CompletedFlight(nil), t.completed[len(t.completed)-1000:]...)
		}
	}
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

func (t *InFlightStore) Recent(limit int) []CompletedFlight {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if limit <= 0 || limit >= len(t.completed) {
		out := make([]CompletedFlight, len(t.completed))
		copy(out, t.completed)
		return out
	}

	start := len(t.completed) - limit
	out := make([]CompletedFlight, len(t.completed[start:]))
	copy(out, t.completed[start:])
	return out
}

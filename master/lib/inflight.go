package lib

import "time"

// struct to track in-flight requests (is not needed for the application logic, but can be useful for monitoring and debugging)
type InFlight struct {
	RequestID string
	Worker    string
	StartedAt time.Time
}

// in-flight request tracking methods (not strictly necessary, but can be useful for monitoring and debugging)
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

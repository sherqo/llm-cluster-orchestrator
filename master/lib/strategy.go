package lib

func (w *Worker) ActiveRequests() int64 {
	return w.activeRequests.Load()
}

func (r *Router) SetStrategy(strategy string) {
	r.strategyM.Lock()
	defer r.strategyM.Unlock()
	r.strategy = strategy
}

func (r *Router) PickWorker(req ChatRequest) (*Worker, error) {
	r.strategyM.RLock()
	strategy := r.strategy
	r.strategyM.RUnlock()

	r.workersM.RLock()
	defer r.workersM.RUnlock()

	switch strategy {
	case StrategyLeastConnections:
		return r.pickLeastConnections()
	default:
		return r.pickWeightedLeastLoad()
	}
}


// strategies for picking workers

// pickLeastConnections picks the worker with the least number of active requests
func (r *Router) pickLeastConnections() (*Worker, error) {
	var best *Worker
	var minActive int64 = -1

	for _, worker := range r.workers {
		if !worker.IsHealthy() {
			continue
		}

		active := worker.ActiveRequests()
		if best == nil || active < minActive {
			best = worker
			minActive = active
		}
	}

	if best == nil {
		return nil, ErrNoWorkersAvailable
	}
	return best, nil
}

// pickWeightedLeastLoad picks the worker with the lowest load score (active requests / weight)
func (r *Router) pickWeightedLeastLoad() (*Worker, error) {
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
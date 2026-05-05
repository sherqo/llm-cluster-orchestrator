package lib

func (w *Worker) ActiveRequests() int64 {
	return w.activeRequests.Load()
}

func (r *Router) SetStrategy(strategy Strategy) {
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
	return r.pickWithStrategy(strategy)
}

func (r *Router) pickWithStrategy(strategy Strategy) (*Worker, error) {
	var pick func() (*Worker, error)

	switch strategy {
	case StrategyLeastConnections:
		pick = r.pickLeastConnections
	case StrategyRoundRobin:
		pick = r.pickRoundRobin
	default:
		pick = r.pickWeightedLeastLoad
	}

	return pick()
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

func (r *Router) pickRoundRobin() (*Worker, error) {
	var selected *Worker
	count := 0

	for _, worker := range r.workers {
		if !worker.IsHealthy() {
			continue
		}
		if count == int(r.rrCounter.Load()) {
			selected = worker
			break
		}
		count++
	}

	if selected == nil {
		// wrap around
		r.rrCounter.Store(0)
		for _, worker := range r.workers {
			if !worker.IsHealthy() {
				continue
			}
			selected = worker
			break
		}
	}

	if selected != nil {
		r.rrCounter.Add(1)
		return selected, nil
	}

	return nil, ErrNoWorkersAvailable
}

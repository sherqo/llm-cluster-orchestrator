# LLM Cluster Orchestrator - Worker Management & Health Monitoring Analysis

## Project Structure
```
/Users/yousseftarek/Documents/projects/llm-cluster-orchestrator/master/
├── main.go                      # Entry point - initializes router and adds workers
├── lib/
│   ├── worker.go               # Worker struct and health state management
│   ├── router.go               # Request routing and circuit recovery logic
│   ├── strategy.go             # Worker selection strategies (round-robin, least-connections, weighted-load)
│   ├── server.go               # HTTP server and request handler
│   ├── inflight.go             # In-flight request tracking
│   └── verbose.go              # Logging utility
├── generated/
│   ├── worker.pb.go            # Generated protobuf code
│   └── worker_grpc.pb.go       # Generated gRPC code
```

---

## 1. WORKER CREATION AND MANAGEMENT

### Worker Initialization
**Location:** `/Users/yousseftarek/Documents/projects/llm-cluster-orchestrator/master/lib/worker.go:43-85`

```go
func NewWorker(id, addr string, weight float64) (*Worker, error) {
    // Creates a new worker with:
    // - Unique ID (e.g., "worker-localhost:50051")
    // - Worker address
    // - Weight for load balancing
    // - gRPC connection with keepalive every 5 seconds
    // - Initial status: WorkerHealthy
    // - Exponential backoff retry (up to 3 attempts, starting at 500ms)
}
```

**Initial Status:** `WorkerHealthy` (line 81)

### Router Worker Management
**Location:** `/Users/yousseftarek/Documents/projects/llm-cluster-orchestrator/master/lib/router.go:51-62`

```go
func (r *Router) AddWorker(addr string) {
    // Creates a new Worker via NewWorker()
    // Stores in router.workers map with ID as key
    // Map protected by RWMutex (workersM)
}
```

**Worker Addition Flow:**
1. Router initialized in `main.go:22`
2. Workers added manually in `main.go:25-27`:
   - `router.AddWorker("localhost:50051")`
   - `router.AddWorker("localhost:50052")`
   - `router.AddWorker("localhost:50053")`

---

## 2. WORKER HEALTH MONITORING

### Worker Status States
**Location:** `/Users/yousseftarek/Documents/projects/llm-cluster-orchestrator/master/lib/worker.go:17-24`

```go
type WorkerStatus string

const (
    WorkerHealthy   WorkerStatus = "healthy"   // Can receive requests
    WorkerSuspected WorkerStatus = "suspected" // Failed requests, not confirmed yet
    WorkerDraining  WorkerStatus = "draining"  // Being drained, no new requests
    WorkerDead      WorkerStatus = "dead"      // Confirmed unhealthy, should be removed
)
```

### Health Check Methods
**Location:** `/Users/yousseftarek/Documents/projects/llm-cluster-orchestrator/master/lib/worker.go:121-133`

```go
func (w *Worker) isRoutable() bool {
    // Private method: Returns true only if status == WorkerHealthy
}

func (w *Worker) IsHealthy() bool {
    // Public method: Returns true only if status == WorkerHealthy
}
```

### gRPC Keepalive (Implicit Health Monitoring)
**Location:** `/Users/yousseftarek/Documents/projects/llm-cluster-orchestrator/master/lib/worker.go:58-62`

```go
grpc.WithKeepaliveParams(keepalive.ClientParameters{
    Time:                5 * time.Second,           // Ping every 5 seconds
    Timeout:             3 * time.Second,           // Wait 3 seconds for response
    PermitWithoutStream: true,                      // Ping even without active streams
})
```

---

## 3. MarkSuspected() AND MarkHealthy() CALLS

### MarkSuspected() Implementation
**Location:** `/Users/yousseftarek/Documents/projects/llm-cluster-orchestrator/master/lib/worker.go:135-147`

```go
func (w *Worker) MarkSuspected() {
    w.mu.Lock()
    defer w.mu.Unlock()
    if w.status == WorkerDead {
        return  // Don't change dead workers
    }
    if w.status == WorkerSuspected {
        w.status = WorkerDead  // Second failure -> Dead
        return
    }
    w.status = WorkerSuspected  // First failure -> Suspected
}
```

**State Transition Logic:**
- `Healthy → Suspected` (first failure)
- `Suspected → Dead` (second failure)
- `Dead → Dead` (no further changes)

### MarkHealthy() - NOT IMPLEMENTED
**Location:** `/Users/yousseftarek/Documents/projects/llm-cluster-orchestrator/master/lib/router.go:78`

```go
worker.MarkHealthy()  // CALLED but NOT DEFINED
```

**Status:** MISSING - Method is called but not implemented in worker.go

### Where MarkSuspected() and MarkHealthy() Are Called
**Location:** `/Users/yousseftarek/Documents/projects/llm-cluster-orchestrator/master/lib/router.go:64-91`

```go
func (r *Router) HandleChat(ctx context.Context, requestID string, req ChatRequest) (ChatResponse, error) {
    var lastErr error
    
    for attemptsLeft := 3; attemptsLeft > 0; attemptsLeft-- {
        worker, err := r.PickWorker(req)
        if err != nil {
            break
        }
        
        r.AddInFlight(requestID, worker.addr)
        reply, sendErr := worker.Send(ctx, requestID, req)
        r.RemoveInFlight(requestID)
        
        if sendErr == nil {
            worker.MarkHealthy()  // LINE 78 - MISSING IMPLEMENTATION
            return ChatResponse{RequestID: requestID, Reply: reply}, nil
        }
        
        worker.MarkSuspected()  // LINE 82 - CALLED WHEN REQUEST FAILS
        lastErr = sendErr
    }
    
    if lastErr != nil {
        return ChatResponse{}, fmt.Errorf("%w: %w", ErrWorkerFailed, lastErr)
    }
    
    return ChatResponse{}, ErrNoWorkersAvailable
}
```

**Call Summary:**
- `MarkHealthy()`: Called after successful request (line 78) - **NOT IMPLEMENTED**
- `MarkSuspected()`: Called after failed request (line 82) - **IMPLEMENTED**
- Retry logic: Up to 3 attempts per request

---

## 4. HEALTH CHECK MECHANISMS AND MONITORING

### Current Health Check Mechanisms

#### A. Reactive Health Checks (Request-Based)
- Health status changes based on request success/failure
- No proactive health probing

#### B. gRPC Keepalive
- Configured at worker connection level
- 5-second ping interval with 3-second timeout
- Automatically detects connection loss but doesn't explicitly mark workers unhealthy

#### C. Circuit Recovery Loop (Incomplete)
**Location:** `/Users/yousseftarek/Documents/projects/llm-cluster-orchestrator/master/lib/router.go:93-110`

```go
func (r *Router) StartCircuitRecoveryLoop() {
    ticker := time.NewTicker(1 * time.Second)
    
    go func() {
        for range ticker.C {
            r.workersM.RLock()
            workers := make([]*Worker, 0, len(r.workers))
            for _, worker := range r.workers {
                workers = append(workers, worker)
            }
            r.workersM.RUnlock()
            
            for _, worker := range workers {
                worker.maybeHalfOpen()  // CALLED but NOT DEFINED
            }
        }
    }()
}
```

**Status:** INCOMPLETE - Method `maybeHalfOpen()` is called but not implemented

**Intended Purpose:** Likely implements circuit breaker half-open state to test if suspected workers have recovered

#### D. In-Flight Request Tracking
**Location:** `/Users/yousseftarek/Documents/projects/llm-cluster-orchestrator/master/lib/inflight.go:1-36`

- Tracks in-flight requests by RequestID
- Records which worker is handling each request
- Records start time of each request
- Useful for monitoring and debugging, not for health decisions

### Missing Health Check Features
1. **MarkHealthy() method** - Not implemented
2. **maybeHalfOpen() method** - Not implemented
3. **StartCircuitRecoveryLoop()** - Called but never invoked in code
4. **No active health probing** - Only reactive based on request failures
5. **No health check endpoint** - No explicit health check to workers

---

## 5. ROUTING DECISIONS BASED ON WORKER STATUS

### Worker Selection Strategies
**Location:** `/Users/yousseftarek/Documents/projects/llm-cluster-orchestrator/master/lib/strategy.go:23-119`

#### A. Least Connections Strategy
```go
func (r *Router) pickLeastConnections() (*Worker, error) {
    var best *Worker
    var minActive int64 = -1
    
    for _, worker := range r.workers {
        if !worker.IsHealthy() {  // SKIP non-healthy workers
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
```

**Routing Logic:**
- Only selects workers with `status == WorkerHealthy`
- Picks worker with lowest number of active requests
- Returns error if no healthy workers available

#### B. Weighted Least Load Strategy
```go
func (r *Router) pickWeightedLeastLoad() (*Worker, error) {
    var best *Worker
    var bestScore float64
    
    for _, worker := range r.workers {
        if !worker.isRoutable() {  // SKIP non-routable (non-healthy) workers
            continue
        }
        
        score := worker.loadScore()  // score = active_requests / weight
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
```

**Routing Logic:**
- Only selects routable (healthy) workers
- Calculates load score: `active_requests / weight`
- Picks worker with lowest load score
- Higher weight workers get more requests proportionally

#### C. Round Robin Strategy
```go
func (r *Router) pickRoundRobin() (*Worker, error) {
    var selected *Worker
    count := 0
    
    for _, worker := range r.workers {
        if !worker.IsHealthy() {  // SKIP non-healthy workers
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
            if !worker.IsHealthy() {  // SKIP non-healthy workers
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
```

**Routing Logic:**
- Cycles through healthy workers in order
- Wraps around when reaching the end
- Increments counter with each selection

### Default Strategy
**Location:** `/Users/yousseftarek/Documents/projects/llm-cluster-orchestrator/master/lib/router.go:43-49`

```go
func NewRouter() *Router {
    return &Router{
        workers:  make(map[string]*Worker),
        inFlight: make(map[string]InFlight),
        strategy: StrategyLeastConnections,  // DEFAULT
    }
}
```

**Default:** Least Connections strategy

### Routing Decision Flow
**Location:** `/Users/yousseftarek/Documents/projects/llm-cluster-orchestrator/master/lib/server.go:36-82`

```
HTTP Request → chatRequestHandler()
    ↓
router.HandleChat()
    ↓
PickWorker(req) → Applies selected strategy
    ↓
Filters workers: Only selects workers with status == WorkerHealthy
    ↓
Returns selected worker or ErrNoWorkersAvailable
    ↓
worker.Send() → Execute request
    ↓
Success: worker.MarkHealthy()    [NOT IMPLEMENTED]
    ↓
Failure: worker.MarkSuspected()  [IMPLEMENTED]
    ↓
Retry with next healthy worker (up to 3 attempts)
```

---

## SUMMARY OF FINDINGS

### Implemented Features
- ✅ Worker creation with gRPC connection
- ✅ Worker status enum (Healthy, Suspected, Draining, Dead)
- ✅ MarkSuspected() - Transitions Healthy→Suspected→Dead
- ✅ IsHealthy() / isRoutable() - Health status checks
- ✅ Three routing strategies with health filtering
- ✅ Request retry logic (up to 3 attempts)
- ✅ gRPC keepalive monitoring
- ✅ In-flight request tracking

### Missing/Incomplete Features
- ❌ **MarkHealthy() method** - Not implemented (called in router.go:78)
- ❌ **maybeHalfOpen() method** - Not implemented (called in router.go:106)
- ❌ **StartCircuitRecoveryLoop() invocation** - Method defined but never called
- ❌ **No active health probing** - Only reactive health checks
- ❌ **No DrainWorker() or RemoveWorker()** - No way to handle Draining/Dead states
- ❌ **No metrics/monitoring** - No health history or failure tracking

### Key Files and Line References

| File | Purpose | Key Lines |
|------|---------|-----------|
| worker.go | Worker struct, health state, MarkSuspected | 17-147 |
| router.go | Request routing, circuit recovery | 51-110 |
| strategy.go | Worker selection strategies | 40-119 |
| server.go | HTTP server, request handling | 36-82 |
| inflight.go | Request tracking | 1-36 |


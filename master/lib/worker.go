package lib

import (
	"context"
	"fmt"
	"time"

	pb "master/generated"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type WorkerStatus string

const (
	WorkerHealthy  WorkerStatus = "healthy"
	WorkerDraining WorkerStatus = "draining"
	WorkerDead     WorkerStatus = "dead"
)


type CircuitState string

const (
	CircuitClosed   CircuitState = "closed"
	CircuitOpen     CircuitState = "open"
	CircuitHalfOpen CircuitState = "half_open"
)

type Worker struct {
	id     string
	addr   string
	weight float64

	activeRequests int64
	queueDepth     int64

	status       WorkerStatus
	circuitState CircuitState
	failures     int64
	successes    int64
	openedAt     time.Time

	client pb.WorkerServiceClient
	conn   *grpc.ClientConn
	mu     sync.RWMutex
}

func NewWorker(id, addr string, weight float64) *(Worker,error) {
	if weight <= 0 {
		weight = 1
	}

	var conn *grpc.ClientConn
	var err error
	// i am keeping the server connection open and it will ping every 5s
	maxAttempts := 3
	backoff := 500 * time.Millisecond

	for i := 0; i < maxAttempts; i++ {
		// dial is lazy so it doesn't gurantee the server is up at this meomment
		conn, err = grpc.Dial(
			addr,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithKeepaliveParams(keepalive.ClientParameters{
				Time:                5 * time.Second,
				Timeout:             3 * time.Second,
				PermitWithoutStream: true,
			}),
		)

		if err == nil {
			break
		}

		time.Sleep(backoff)  
		backoff *= 2 // exponential backoff
	}

	if err != nil {
		return nil, fmt.Errorf("failed to connect after retries: %w", err)
	}

	return &Worker{
		id:           id,
		addr:         addr,
		weight:       weight,
		status:       WorkerHealthy,
		circuitState: CircuitClosed,
		conn:         conn,
		client:       pb.NewWorkerServiceClient(conn),
	},nil
}

func (w *Worker) Send(ctx context.Context, requestID string, req ChatRequest) (string, error) {
	atomic.AddInt64(&w.activeRequests, 1)
	defer atomic.AddInt64(&w.activeRequests, -1)
	//TODO: the timeout should be tier aware so pro get longer timouts than free users
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	resp, err := w.client.Handle(ctx, &pb.Request{
		RequestId: requestID,
		Message:   req.Prompt,
		Priority:  tierToPriority(req.Tier),
	})
	if err != nil {
		//TODO: beter error handling here to add priority to certain errors
		//TODO: add retry
		w.recordFailure()
		return "", err
	}

	atomic.StoreInt64(&w.queueDepth, int64(resp.QueueLength))
	w.recordSuccess()

	return resp.Reply, nil
}

// idk about this 
func (w *Worker) loadScore() float64 {
	active := atomic.LoadInt64(&w.activeRequests)
	queue := atomic.LoadInt64(&w.queueDepth)

	w.mu.RLock()
	weight := w.weight
	w.mu.RUnlock()

	return float64(active+queue) / weight
}

func (w *Worker) isRoutable() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()

	if w.status != WorkerHealthy {
		return false
	}

	switch w.circuitState {
	case CircuitClosed:
		return true
	case CircuitHalfOpen:
		return atomic.LoadInt64(&w.activeRequests) == 0
	default:
		return false
	}
}

func (w *Worker) maybeHalfOpen() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.circuitState != CircuitOpen {
		return
	}

	if time.Since(w.openedAt) >= 10*time.Second {
		w.circuitState = CircuitHalfOpen
	}
}

func (w *Worker) recordFailure() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.circuitState == CircuitHalfOpen {
		w.circuitState = CircuitOpen
		w.openedAt = time.Now()
		return
	}

	w.failures++
	//handling it softly
	total := w.failures + w.successes
	if total >= 20 {
		failureRate := float64(w.failures) / float64(total)
		if failureRate > 0.50 {
			w.circuitState = CircuitOpen
			w.openedAt = time.Now()
			w.failures = 0
			w.successes = 0
		}
	}
}

func (w *Worker) IsAvailable() bool {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.maybeHalfOpen()

	if w.status != WorkerHealthy {
		return false
	}

	if w.circuitState == CircuitOpen {
		return false
	}

	return true
}


func (w *Worker) recordSuccess() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.circuitState == CircuitHalfOpen {
		w.circuitState = CircuitClosed
		w.failures = 0
		w.successes = 0
		return
	}

	w.successes++
}
// higher is higher and "pro" users get higher priority than "free" users. Adjust as needed.
func tierToPriority(tier string) int32 {
	switch tier {
	case "pro":
		return 100
	case "free":
		return 50
	default:
		return 50
	}
}

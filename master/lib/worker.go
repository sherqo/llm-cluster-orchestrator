package lib

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	pb "master/generated"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

type WorkerStatus string

const (
	WorkerHealthy   WorkerStatus = "healthy"   // worker is healthy and can receive requests
	WorkerSuspected WorkerStatus = "suspected" // worker is suspected to be unhealthy (e.g. failed requests) but not confirmed yet
	WorkerDraining  WorkerStatus = "draining"  // worker is being drained and should not receive new requests, but can finish existing ones and then be removed
	WorkerDead      WorkerStatus = "dead"      // worker is confirmed to be unhealthy and should be removed
)

// worker struct represents a worker server
type Worker struct {
	id     string  // unique identifier for the worker, eg: "worker-localhost:50051"
	addr   string  // address of the worker server, eg: "localhost:50051"
	weight float64 // weight for load balancing, higher means more requests will be routed to this worker

	activeRequests atomic.Int64

	status              WorkerStatus
	statusChangedAt     time.Time // timestamp when status last changed
	consecutiveFailures int64     // track consecutive failures to determine if suspected

	// gRPC client and connection
	client pb.WorkerServiceClient
	conn   *grpc.ClientConn
	mu     sync.RWMutex
}

// constructor
func NewWorker(id, addr string, weight float64) (*Worker, error) {
	if weight <= 0 {
		weight = 1
	}

	var conn *grpc.ClientConn
	var err error
	// i am keeping the server connection open and it will ping every 5s
	maxAttempts := 3
	backoff := 500 * time.Millisecond

	for i := 0; i < maxAttempts; i++ {
		conn, err = grpc.NewClient(
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
		id:             id,
		addr:           addr,
		weight:         weight,
		status:         WorkerHealthy,
		statusChangedAt: time.Now(),
		conn:           conn,
		client:         pb.NewWorkerServiceClient(conn),
	}, nil
}

// method to send a request to the worker and get a response
func (w *Worker) Send(ctx context.Context, requestID string, req ChatRequest) (string, error) {
	w.activeRequests.Add(1)
	defer w.activeRequests.Add(-1)

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
		return "", err
	}

	return resp.Reply, nil
}

// idk about this
func (w *Worker) loadScore() float64 {
	active := w.activeRequests.Load()

	w.mu.RLock()
	weight := w.weight
	w.mu.RUnlock()

	return float64(active) / weight
}

func (w *Worker) isRoutable() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return w.status == WorkerHealthy
}

func (w *Worker) IsHealthy() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return w.status == WorkerHealthy
}

func (w *Worker) GetStatus() WorkerStatus {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.status
}

func (w *Worker) MarkSuspected() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.status == WorkerDead {
		return
	}

	if w.status == WorkerSuspected {
		w.status = WorkerDead
		w.statusChangedAt = time.Now()
		return
	}
	w.status = WorkerSuspected
	w.statusChangedAt = time.Now()
	w.consecutiveFailures++
}

func (w *Worker) MarkHealthy() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.status == WorkerDead {
		return
	}
	w.status = WorkerHealthy
	w.statusChangedAt = time.Now()
	w.consecutiveFailures = 0
}

func (w *Worker) MarkDraining() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.status == WorkerDead {
		return
	}
	w.status = WorkerDraining
	w.statusChangedAt = time.Now()
}

// MaybeRecoverFromSuspected attempts to recover a suspected worker back to healthy status
// after a timeout period has elapsed. This allows temporary failures to not permanently kill workers.
func (w *Worker) MaybeRecoverFromSuspected() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.status != WorkerSuspected {
		return
	}

	// If suspected for more than 30 seconds, try to recover it back to healthy
	if time.Since(w.statusChangedAt) >= 30*time.Second {
		w.status = WorkerHealthy
		w.statusChangedAt = time.Now()
		w.consecutiveFailures = 0
	}
}

// MaybeResurrectFromDead attempts to resurrect a dead worker after a long timeout period
// This is a last-resort recovery mechanism for workers that have been dead for a long time
func (w *Worker) MaybeResurrectFromDead() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.status != WorkerDead {
		return
	}

	// If dead for more than 5 minutes, try to resurrect it back to suspected
	// so it gets another chance
	if time.Since(w.statusChangedAt) >= 5*time.Minute {
		w.status = WorkerSuspected
		w.statusChangedAt = time.Now()
		w.consecutiveFailures = 0
	}
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

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
	id     string // unique identifier for the worker, eg: "worker-localhost:50051"
	addr   string // address of the worker server, eg: "localhost:50051"

	activeRequests atomic.Int64

	status    WorkerStatus
	failures  int64
	successes int64

	// gRPC client and connection
	client pb.WorkerServiceClient
	conn   *grpc.ClientConn
	mu     sync.RWMutex
}

// constructor
func NewWorker(id, addr string) (*Worker, error) {
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
		id:     id,
		addr:   addr,
		status: WorkerHealthy,
		conn:   conn,
		client: pb.NewWorkerServiceClient(conn),
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
		w.recordFailure()
		return "", err
	}

	w.recordSuccess()

	return resp.Reply, nil
}

func (w *Worker) recordFailure() {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.failures++
}

func (w *Worker) IsHealthy() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return w.status == WorkerHealthy
}

func (w *Worker) MarkSuspected() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.status == WorkerDead {
		return
	}

	if w.status == WorkerSuspected {
		w.status = WorkerDead
		return
	}
	w.status = WorkerSuspected
}

func (w *Worker) MarkHealthy() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.status == WorkerDead {
		return
	}
	w.status = WorkerHealthy
}

func (w *Worker) recordSuccess() {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.successes++
}

func (w *Worker) Snapshot() WorkerSnapshot {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return WorkerSnapshot{
		ID:             w.id,
		Addr:           w.addr,
		Status:         w.status,
		Failures:       w.failures,
		Successes:      w.successes,
		ActiveRequests: w.activeRequests.Load(),
	}
}

func (w *Worker) SetDraining() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.status == WorkerDead {
		return
	}
	w.status = WorkerDraining
}

func (w *Worker) Close() error {
	if w.conn == nil {
		return nil
	}
	return w.conn.Close()
}

func (w *Worker) ID() string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.id
}

func (w *Worker) Addr() string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.addr
}

func (w *Worker) Snapshot() WorkerSnapshot {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return WorkerSnapshot{
		ID:             w.id,
		Addr:           w.addr,
		Weight:         w.weight,
		Status:         w.status,
		CircuitState:   w.circuitState,
		Failures:       w.failures,
		Successes:      w.successes,
		ActiveRequests: w.activeRequests.Load(),
		LoadScore:      float64(w.activeRequests.Load()) / w.weight,
	}
}

func (w *Worker) SetDraining() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.status == WorkerDead {
		return
	}
	w.status = WorkerDraining
}

func (w *Worker) Close() error {
	if w.conn == nil {
		return nil
	}
	return w.conn.Close()
}

func (w *Worker) ID() string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.id
}

func (w *Worker) Addr() string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.addr
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

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

// ---------------------------------------------------------------------------
// Worker lifecycle states (#6)
// ---------------------------------------------------------------------------

// WorkerStatus is kept for backward compatibility with snapshots.
type WorkerStatus string

const (
	WorkerHealthy  WorkerStatus = "healthy"  // worker is healthy and can receive requests
	WorkerDraining WorkerStatus = "draining" // worker is being drained and should not receive new requests, but can finish existing ones and then be removed
)

// LifecycleState represents the full lifecycle of a worker (#6).
type LifecycleState int

const (
	StateStarting LifecycleState = iota // worker container created, gRPC not yet connected
	StateWarming                        // gRPC connected, worker warming up (loading model, etc.)
	StateHealthy                        // fully ready to serve requests
	StateDraining                       // no new requests routed, waiting for active to finish
	StateStopping                       // active requests done, closing connection
	StateDead                           // fully terminated, ready for removal
)

func (ls LifecycleState) String() string {
	switch ls {
	case StateStarting:
		return "STARTING"
	case StateWarming:
		return "WARMING"
	case StateHealthy:
		return "HEALTHY"
	case StateDraining:
		return "DRAINING"
	case StateStopping:
		return "STOPPING"
	case StateDead:
		return "DEAD"
	default:
		return "UNKNOWN"
	}
}

// ToWorkerStatus converts LifecycleState to the legacy WorkerStatus for backward compat.
func (ls LifecycleState) ToWorkerStatus() WorkerStatus {
	switch ls {
	case StateHealthy:
		return WorkerHealthy
	case StateDraining, StateStopping:
		return WorkerDraining
	default:
		return WorkerDraining // non-routable states
	}
}

// ---------------------------------------------------------------------------
// Worker struct
// ---------------------------------------------------------------------------

// worker struct represents a worker server
type Worker struct {
	id   string // unique identifier for the worker, eg: "worker-localhost:50051"
	addr string // address of the worker server, eg: "localhost:50051"

	activeRequests atomic.Int64

	lifecycle LifecycleState // new full lifecycle state (#6)
	status    WorkerStatus   // kept for backward compat

	agentID string // which agent spawned this worker (#10)

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
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		conn, err = grpc.DialContext(
			ctx,
			addr,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithKeepaliveParams(keepalive.ClientParameters{
				Time:                5 * time.Second,
				Timeout:             3 * time.Second,
				PermitWithoutStream: true,
			}),
			grpc.WithBlock(),
		)
		cancel()

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
		id:        id,
		addr:      addr,
		status:    WorkerHealthy,
		lifecycle: StateHealthy,
		conn:      conn,
		client:    pb.NewWorkerServiceClient(conn),
	}, nil
}

// method to send a request to the worker and get a response
func (w *Worker) Send(ctx context.Context, requestID string, req ChatRequest) (string, error) {
	w.activeRequests.Add(1)
	defer w.activeRequests.Add(-1)

	//TODO: the timeout should be tier aware so pro get longer timouts than free users
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
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

func (w *Worker) Ping() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := w.client.Ping(ctx, &pb.PingRequest{})
	return err
}

// isRoutable returns true only for workers that can accept new requests (#6).
func (w *Worker) isRoutable() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return w.lifecycle == StateHealthy
}

func (w *Worker) IsHealthy() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return w.lifecycle == StateHealthy
}

// ---------------------------------------------------------------------------
// Lifecycle state accessors (#6)
// ---------------------------------------------------------------------------

func (w *Worker) GetLifecycleState() LifecycleState {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.lifecycle
}

func (w *Worker) SetLifecycleState(state LifecycleState) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.lifecycle = state
	w.status = state.ToWorkerStatus()
}

func (w *Worker) GetStatus() WorkerStatus {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.status
}

func (w *Worker) MarkHealthy() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.lifecycle = StateHealthy
	w.status = WorkerHealthy
}

func (w *Worker) MarkDraining() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.lifecycle = StateDraining
	w.status = WorkerDraining
}

func (w *Worker) Snapshot() WorkerSnapshot {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return WorkerSnapshot{
		ID:             w.id,
		Addr:           w.addr,
		Status:         w.status,
		Lifecycle:      w.lifecycle,
		Failures:       0,
		Successes:      0,
		ActiveRequests: w.activeRequests.Load(),
		AgentID:        w.agentID,
	}
}

func (w *Worker) SetDraining() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.lifecycle = StateDraining
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

// ---------------------------------------------------------------------------
// Agent tracking (#10)
// ---------------------------------------------------------------------------

func (w *Worker) SetAgentID(agentID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.agentID = agentID
}

func (w *Worker) AgentID() string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.agentID
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

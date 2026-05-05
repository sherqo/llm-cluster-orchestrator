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
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	resp, err := w.client.Handle(ctx, &pb.Request{
		RequestId: requestID,
		Message:   req.Prompt,
		Priority:  tierToPriority(req.Tier),
	})
	if err != nil {
		return "", err
	}

	return resp.Reply, nil
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

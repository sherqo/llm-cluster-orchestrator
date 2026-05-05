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

func NewWorker(addr string) *Worker {
	conn, err := grpc.Dial(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		panic(fmt.Sprintf("failed to connect to worker %s: %v", addr, err))
	}

	return &Worker{
		addr:   addr,
		conn:   conn,
		client: pb.NewWorkerServiceClient(conn),
	}
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

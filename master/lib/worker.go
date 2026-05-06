package lib

import (
	"context"
	"fmt"
	"time"

	pb "master/generated"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Worker struct {
	addr   string
	client pb.WorkerServiceClient
	conn   *grpc.ClientConn
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
	ctx, cancel := context.WithTimeout(ctx, 300*time.Second)
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

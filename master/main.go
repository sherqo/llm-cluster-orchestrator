/*
* The LB should listen to http in some port and then should try to response for it
 */

package main

import (
	"context"
	"log"
	"time"

	pb "master/generated" // adjust this

	"google.golang.org/grpc"
)

func main() {
	conn, err := grpc.Dial("localhost:50051", grpc.WithInsecure())
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	client := pb.NewWorkerServiceClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*2)
	defer cancel()

	resp, err := client.Handle(ctx, &pb.Request{
		RequestId: "1",
		Message:   "hello from LB",
		Priority:  1,
	})

	if err != nil {
		log.Fatal(err)
	}

	log.Println("reply:", resp.Reply)
	log.Println("queue:", resp.QueueLength)
}
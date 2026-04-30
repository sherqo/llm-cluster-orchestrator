def handle_request():
    print("Handling request in worker...")


import grpc
from concurrent import futures


import worker_pb2
import worker_pb2_grpc


class Worker(worker_pb2_grpc.WorkerServiceServicer):
    def Handle(self, request, context):
        print("received:", request.message)

        return worker_pb2.Response(
            reply="processed: " + request.message,
            queue_length=1,
        )


def serve():
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=10))

    worker_pb2_grpc.add_WorkerServiceServicer_to_server(Worker(), server)

    server.add_insecure_port("[::]:50051")
    server.start()

    print("worker running on :50051")

    server.wait_for_termination()


if __name__ == "__main__":
    serve()

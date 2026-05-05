import grpc
import sys
import heapq
import threading
from concurrent import futures

import worker_pb2
import worker_pb2_grpc
from rag import retrieve
from model import run_model

# ─────────────────────────────────────────────
# CONFIG
# python main.py 50051
# python main.py 50052
# ─────────────────────────────────────────────
WORKER_PORT = int(sys.argv[1]) if len(sys.argv) > 1 else 50051
 

# ─────────────────────────────────────────────
# PRIORITY QUEUE (in-memory)
# pro  → priority 0 → served first
# free → priority 1 → served after
# ─────────────────────────────────────────────
class PriorityQueue:
    def __init__(self):
        self._queue = []
        self._lock = threading.Lock()

    def push(self, priority, request_id, message):
        with self._lock:
            heapq.heappush(self._queue, (priority, request_id, message))

    def pop(self):
        with self._lock:
            if self._queue:
                return heapq.heappop(self._queue)
            return None

    def size(self):
        with self._lock:
            return len(self._queue)


# ─────────────────────────────────────────────
# WORKER — handles gRPC requests from master
# ─────────────────────────────────────────────
class Worker(worker_pb2_grpc.WorkerServiceServicer):

    def __init__(self):
        self.queue = PriorityQueue()

    def Handle(self, request, context):
        # Step 1: determine priority from tier
        # master sends 0 for pro, 1 for free
        priority = request.priority if request.priority in (0, 1) else 1
        tier = "pro" if priority == 0 else "free"

        print(f"[worker:{WORKER_PORT}] received  request_id={request.request_id} tier={tier}")

        # Step 2: push into priority queue
        self.queue.push(priority, request.request_id, request.message)

        # Step 3: pop highest priority item
        item = self.queue.pop()
        if item is None:
            context.abort(grpc.StatusCode.INTERNAL, "queue unexpectedly empty")
            return

        _, req_id, message = item
        print(f"[worker:{WORKER_PORT}] handling  request_id={req_id}")

        # Step 4: RAG — retrieve relevant context from ChromaDB
        rag_context = retrieve(message)

        # Step 5: run model (mock or real)
        reply = run_model(prompt=message, context=rag_context, worker_port=WORKER_PORT)

        # Step 6: return response + current queue depth (piggybacked)
        depth = self.queue.size()
        print(f"[worker:{WORKER_PORT}] done      request_id={req_id} queue_depth={depth}")

        return worker_pb2.Response(
            request_id=req_id,
            reply=reply,
            queue_length=depth,
        )


# ─────────────────────────────────────────────
# MAIN
# ─────────────────────────────────────────────
def serve():
    worker = Worker()

    server = grpc.server(futures.ThreadPoolExecutor(max_workers=10))
    worker_pb2_grpc.add_WorkerServiceServicer_to_server(worker, server)
    server.add_insecure_port(f"[::]:{WORKER_PORT}")
    server.start()
    print(f"[worker:{WORKER_PORT}] running")
    server.wait_for_termination()


if __name__ == "__main__":
    serve()
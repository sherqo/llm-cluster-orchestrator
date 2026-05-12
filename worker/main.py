import grpc
import heapq
import sys
import threading
from concurrent import futures

import requests
import worker_pb2
import worker_pb2_grpc
from rag import retrieve
from model import run_model, get_ollama_url, OLLAMA_MODEL

# ─────────────────────────────────────────────
# CONFIG
# ─────────────────────────────────────────────
import os

worker_port_env = os.getenv("WORKER_PORT")
if worker_port_env:
    WORKER_PORT = int(worker_port_env)
else:
    WORKER_PORT = int(sys.argv[1]) if len(sys.argv) > 1 else 50051


def _is_ollama_alive(url: str) -> bool:
    """Return True if an Ollama server is reachable at this URL."""
    try:
        response = requests.get(f"{url}/api/tags", timeout=2)
        return response.status_code == 200
    except requests.RequestException:
        return False


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

    def Ping(self, request, context):
        return worker_pb2.PingResponse(status="ok")

    def Handle(self, request, context):
        #  determine tier from priority (inverted: lower num = higher priority for heapq)
        # master sends: elite=10000, pro=100, free=50
        # convert to heapq-friendly: elite=1, pro=2, free=3
        if request.priority >= 1000:
            tier = "elite"
            priority = 1  # highest priority for heapq
        elif request.priority >= 50:
            tier = "pro"
            priority = 2
        else:
            tier = "free"
            priority = 3

        print(
            f"[worker:{WORKER_PORT}] received  request_id={request.request_id} tier={tier}"
        )

        #  push into priority queue
        self.queue.push(priority, request.request_id, request.message)

        #  pop highest priority item
        item = self.queue.pop()
        if item is None:
            context.abort(grpc.StatusCode.INTERNAL, "queue unexpectedly empty")
            return

        _, req_id, message = item
        print(f"[worker:{WORKER_PORT}] handling  request_id={req_id}")

        #  Tier-based config: elite gets RAG + 300 tokens, others get no RAG + 20 tokens
        use_rag = (tier == "elite")
        num_predict = 300 if tier == "elite" else 20

        #  RAG — retrieve relevant context only for elite users
        rag_context = retrieve(message) if use_rag else ""

        #  run this worker's private Ollama model instance
        reply = run_model(prompt=message, context=rag_context, worker_port=WORKER_PORT, num_predict=num_predict)

        #  return response + current queue depth (piggybacked)
        depth = self.queue.size()
        print(
            f"[worker:{WORKER_PORT}] done      request_id={req_id} queue_depth={depth}"
        )

        return worker_pb2.Response(
            request_id=req_id,
            reply=reply,
            queue_length=depth,
        )


# ─────────────────────────────────────────────
# MAIN
# ─────────────────────────────────────────────
def serve():
    ollama_url = get_ollama_url(WORKER_PORT)
    if not _is_ollama_alive(ollama_url):
        print(f"[worker:{WORKER_PORT}] WARNING: Ollama not reachable at {ollama_url}")

    worker = Worker()

    server = grpc.server(futures.ThreadPoolExecutor(max_workers=10))
    worker_pb2_grpc.add_WorkerServiceServicer_to_server(worker, server)
    server.add_insecure_port(f"0.0.0.0:{WORKER_PORT}")
    server.start()
    print(f"[worker:{WORKER_PORT}] running")
    server.wait_for_termination()


if __name__ == "__main__":
    serve()

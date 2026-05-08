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
# python main.py 50051
# python main.py 50052
# ─────────────────────────────────────────────
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

    def Handle(self, request, context):
        #  determine priority from tier
        # master sends 0 for pro, 1 for free
        priority = request.priority if request.priority in (0, 1) else 1
        tier = "pro" if priority == 0 else "free"

        print(f"[worker:{WORKER_PORT}] received  request_id={request.request_id} tier={tier}")

        #  push into priority queue
        self.queue.push(priority, request.request_id, request.message)

        #  pop highest priority item
        item = self.queue.pop()
        if item is None:
            context.abort(grpc.StatusCode.INTERNAL, "queue unexpectedly empty")
            return

        _, req_id, message = item
        print(f"[worker:{WORKER_PORT}] handling  request_id={req_id}")

        #  RAG — retrieve relevant context from ChromaDB
        rag_context = retrieve(message)

        #  run this worker's private Ollama model instance
        reply = run_model(prompt=message, context=rag_context, worker_port=WORKER_PORT)

        #  return response + current queue depth (piggybacked)
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
    ollama_url = get_ollama_url(WORKER_PORT)
    if not _is_ollama_alive(ollama_url):
        print(f"[worker:{WORKER_PORT}] WARNING: Ollama not reachable at {ollama_url}")

    worker = Worker()

    server = grpc.server(futures.ThreadPoolExecutor(max_workers=10))
    worker_pb2_grpc.add_WorkerServiceServicer_to_server(worker, server)
    server.add_insecure_port(f"localhost:{WORKER_PORT}")
    server.start()
    print(f"[worker:{WORKER_PORT}] running")
    server.wait_for_termination()


if __name__ == "__main__":
    serve()

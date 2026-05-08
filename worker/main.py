import atexit
import grpc
import heapq
import os
import subprocess
import sys
import threading
import time
from concurrent import futures
from pathlib import Path

import requests
import worker_pb2
import worker_pb2_grpc
from rag import retrieve
from model import run_model, get_ollama_port, get_ollama_url, OLLAMA_MODEL

# ─────────────────────────────────────────────
# CONFIG
# python main.py 50051
# python main.py 50052
# ─────────────────────────────────────────────
WORKER_PORT = int(sys.argv[1]) if len(sys.argv) > 1 else 50051
OLLAMA_PROCESS = None


# ─────────────────────────────────────────────
# PRIVATE OLLAMA INSTANCE PER WORKER
# Worker 50051 -> Ollama 11435
# Worker 50052 -> Ollama 11436
# Worker 50053 -> Ollama 11437
# ─────────────────────────────────────────────
def _is_ollama_alive(url: str) -> bool:
    """Return True if an Ollama server is already reachable at this URL."""
    try:
        response = requests.get(f"{url}/api/tags", timeout=2)
        return response.status_code == 200
    except requests.RequestException:
        return False


def start_private_ollama_instance():
    """Start the private Ollama server assigned to this worker port."""
    global OLLAMA_PROCESS

    ollama_url = get_ollama_url(WORKER_PORT)
    ollama_port = get_ollama_port(WORKER_PORT)

    if _is_ollama_alive(ollama_url):
        print(f"[worker:{WORKER_PORT}] private Ollama already running at {ollama_url}")
        return

    project_root = Path(__file__).resolve().parents[1]
    logs_dir = project_root / "logs"
    logs_dir.mkdir(exist_ok=True)
    log_path = logs_dir / f"ollama_{ollama_port}.log"
    log_file = open(log_path, "a", encoding="utf-8")

    env = os.environ.copy()
    env["OLLAMA_HOST"] = f"127.0.0.1:{ollama_port}"

    print(
        f"[worker:{WORKER_PORT}] starting private Ollama at {ollama_url} "
        f"with model '{OLLAMA_MODEL}'"
    )

    try:
        OLLAMA_PROCESS = subprocess.Popen(
            ["ollama", "serve"],
            env=env,
            stdout=log_file,
            stderr=subprocess.STDOUT,
        )
    except FileNotFoundError:
        print(
            f"[worker:{WORKER_PORT}] ERROR: 'ollama' command was not found. "
            "Install Ollama and reopen PowerShell."
        )
        return

    # Wait until this private Ollama server is ready.
    for _ in range(60):
        if _is_ollama_alive(ollama_url):
            print(f"[worker:{WORKER_PORT}] private Ollama ready at {ollama_url}")
            return
        time.sleep(1)

    print(
        f"[worker:{WORKER_PORT}] WARNING: private Ollama did not become ready at {ollama_url}. "
        f"Check {log_path}"
    )


def stop_private_ollama_instance():
    """Stop the Ollama process started by this worker when the worker exits."""
    global OLLAMA_PROCESS

    if OLLAMA_PROCESS is not None and OLLAMA_PROCESS.poll() is None:
        print(f"[worker:{WORKER_PORT}] stopping private Ollama")
        OLLAMA_PROCESS.terminate()


atexit.register(stop_private_ollama_instance)


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
    start_private_ollama_instance()

    worker = Worker()

    server = grpc.server(futures.ThreadPoolExecutor(max_workers=10))
    worker_pb2_grpc.add_WorkerServiceServicer_to_server(worker, server)
    server.add_insecure_port(f"localhost:{WORKER_PORT}")
    server.start()
    print(f"[worker:{WORKER_PORT}] running")
    server.wait_for_termination()


if __name__ == "__main__":
    serve()
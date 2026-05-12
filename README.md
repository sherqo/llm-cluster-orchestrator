# LLM Cluster Orchestrator

Distributed LLM inference system designed to handle high concurrency with dynamic load balancing, autoscaling, and fault tolerance.

## Architecture & Stack

The project is built around a Master-Agent-Worker architecture:

- **Master & Load Balancer (Go)**: Single source of truth. Maintains worker registries, monitors queue depths, and handles all incoming HTTP requests. Routes requests to workers via gRPC using least-connections or round-robin strategies. Evaluates cluster metrics to autoscale workers dynamically. Also features a built-in TUI for cluster monitoring.
- **Agent (Go)**: Deployed on each physical machine/node. Dumb executor that connects to the Master. Automatically manages Dockerized worker containers (spawn/kill), monitors host CPU/RAM metrics, and ensures local services like Ollama and ChromaDB are running.
- **Worker (Python)**: Stateless gRPC server running in a Docker container. Responsible for the actual inference process. Uses **Sentence-Transformers** for embedding queries and looking up context in **ChromaDB** (RAG), then formats the prompt and queries the local **Ollama** model for generation.
- **Vector Database (ChromaDB)**: Runs in a Docker container, providing read-only access to embedded document chunks for the workers to use in Retrieval-Augmented Generation (RAG).
- **LLM Inference (Ollama)**: Local LLM runner. By default, the system uses the `smollm:135m` model for fast, local inference.

## Prerequisites & Installation

To run this project locally, you need the following tools installed on your host machine:

1. **Go (1.20+)**: Required for the Master and Agent. [Install Go](https://go.dev/doc/install).
2. **Python (3.10+)**: Required for client scripts. [Install Python](https://www.python.org/downloads/).
3. **Docker & Docker Compose**: Required for running Worker containers and ChromaDB. [Install Docker](https://docs.docker.com/get-docker/).
4. **Ollama**: Required for running the actual LLM models locally. [Install Ollama](https://ollama.com/download).

## Quick Start (Local Run)

This is the exact sequence to bring up the entire stack on a single machine.

### 1. One-time Setup

First, build the Python worker Docker image:
```bash
docker build -t llm-worker:latest ./worker
```

Next, pre-pull the default Ollama model to save time during runtime:
```bash
ollama pull smollm:135m
```

*(Note: The `worker` image already includes the necessary Python dependencies for gRPC and Sentence-Transformers.)*

### 2. Start the Master

The Master handles HTTP requests and orchestrates the cluster.
```bash
cd master
go run .
```
*The master will start listening on `http://127.0.0.1:8080` and launch a Terminal UI (TUI) for monitoring.*

### 3. Start the Agent

Open a **new terminal tab/window** and start the local agent:
```bash
cd agent
go run . --master-url http://127.0.0.1:8080
```

The agent will automatically:
- Start Ollama if it is not already running.
- Start ChromaDB via `docker-compose` in the `vector-db/` directory.
- Register itself with the master.
- Spawn an initial worker container to handle requests.

### 4. Send a Test Request

Once the worker is registered (visible in the Master's TUI), you can test the system:
```bash
curl -X POST http://127.0.0.1:8080/chat \
  -H "Content-Type: application/json" \
  -d '{"userId":"u1","prompt":"What is the battle of stalingrad?","tier":"free"}'
```

## Advanced Usage & Load Testing

### Multi-Agent / Multi-Node
You can launch multiple agents on different machines. Make sure to point them to the master node's IP:
```bash
cd agent
go run . --master-url http://<MASTER_IP>:8080
```
Alternatively, for local simulation on Linux, you can run `scripts/run-linux.sh` to spawn multiple local agents with isolated Ollama ports.

### Load Testing
The `scripts/` directory contains various load testing tools:
- **Simple 1000-request load**: `./scripts/stress_test.sh`
- **Gradual stress test (phased bursts)**: `./scripts/gradual_stress_test.sh`
- **Configurable sweep with JSON output**: `./scripts/run_load_sweep.sh results/run1`
- **Go Client Loadtest**: `go run ./client/loadtest/main.go -master-url http://localhost:8080 -user-id load-1`

## Troubleshooting

- **Chroma Build Fails**: Verify the Docker daemon is running (`docker info`).
- **Connection Refused on Chat**: Verify Ollama is running successfully on your host: `curl http://127.0.0.1:11434/api/tags`.
- **Model Not Found**: Ensure you pulled the model before running: `ollama pull smollm:135m`.
- **Worker Fails to Register**: Check the Master TUI logs or Agent logs to ensure the worker container started successfully and can reach the Master's IP.

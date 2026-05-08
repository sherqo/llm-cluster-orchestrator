# Local Run (Master + Agent + Worker + Chroma)

This guide is the exact sequence used to bring everything up on one machine.

## Requirements

- Docker daemon running
- `docker-compose` available
- Ollama installed (`ollama` in PATH)

## One-time setup

1) Build the worker image (needs CPU torch):

```bash
docker build -t llm-worker:latest ./worker
```

2) (Optional) Pre-pull the Ollama model:

```bash
ollama pull smollm:135m
```

## Start the master

```bash
cd master
go run .
```

## Start the agent

```bash
cd ../agent
go run . --master-url http://127.0.0.1:8080
```

The agent will:

- Start Ollama if it is not running.
- Start Chroma via Docker compose in `vector-db/`.
- Register itself with the master (with backoff if master is not ready yet).

## Spawn a worker

```bash
curl -X POST http://127.0.0.1:9000/workers/create
```

## Send a test request

```bash
curl -X POST http://127.0.0.1:8080/chat \
  -H "Content-Type: application/json" \
  -d '{"userId":"u1","prompt":"What is the battle of stalingrad?","tier":"free"}'
```

## Troubleshooting

- If Chroma build fails, verify Docker is running.
- If worker creation fails, ensure `llm-worker:latest` exists.
- If chat fails with `connect: connection refused`, verify the worker container is running and the master has added it.

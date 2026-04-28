# Distributed LLM System — Final Architecture

## Overview

This distributed system handles 1000+ concurrent LLM inference requests with load balancing, fault tolerance, and auto-scaling across multiple machines. The Master is the single source of truth and controller; Agents are dumb executors. gRPC is used for internal, hot-path communication.

## Table of contents

- [Components](#components)
- [Databases](#databases)
- [Communication](#communication)
- [Failure detection](#failure-detection)
- [Fault tolerance flow](#fault-tolerance-flow)
- [Auto-scaling](#auto-scaling)
- [Worker identity](#worker-identity)
- [Startup sequence](#startup-sequence)
- [Key design principles](#key-design-principles)

## Components

### 1. Client layer

- Up to 1000 concurrent users (pro and free).
- Communicates only via HTTP; clients are unaware of internal topology.

### 2. Master & Load Balancer (Go) — single process

The binary exposes two logical modules: the Master Controller and the Load Balancer (LB).

#### Master controller

- Single source of truth for system state.
- Maintains worker and agent registries.
- Detects worker failures via gRPC errors/keepalive.
- Makes auto-scaling decisions based on LB queue depth and agent-reported CPU/RAM.
- Instructs agents to spawn/kill workers.
- Writes metrics/request logs asynchronously (never in hot path).
- Exposes a live dashboard (workers, queue depths, latency, health).

#### Load Balancer

- Receives all external HTTP requests and routes them to workers over gRPC.
- Routing strategies: Round Robin, Least Connections.
- Priority queueing for pro users (preempt ahead of free users).
- Tracks in-flight requests and queue_depth per worker in memory.
- Piggybacks queue_depth from worker gRPC responses to correct drift.
- On worker death: retrieves in-flight requests and retries them on alive workers.
- Request timeout: 8s, with retries on failure.
- LB never makes cluster-level control decisions — routing only.

Worker state kept by LB (in memory):

```golang
Worker {
    id
    address
    status
    queue_depth
    in_flight_requests: map[reqID] -> { request_data, sent_at }
}
```

### 3. Worker nodes (Python) — e.g. 10.0.0.x:8001

- Each worker is an independent process, spawned by the agent on Master's request.
- Workers register with Master on startup (example payload):

```http
POST http://master:9000/workers/register
{
    "id": "worker-8001-1714123456",
    "address": "10.0.0.2:8001"
}
```

- Worker behavior:
  - Maintains an in-memory priority queue (pro first, FIFO within tier).
  - For each request: embed query (sentence-transformers), FAISS lookup → top-k chunks, build prompt, run LLM inference, return result and current `queue_depth` in the gRPC response.
  - LLM inference is mocked by a sleep (0.3–1.2s) and is pluggable for a real inference backend.
  - Fully stateless — safe to kill and replace.

### 4. Agents (Python) — one per physical machine, e.g. 10.0.0.x:9000

- Dumb, stateless executor; receives control commands from Master only.
- Registers with Master on startup (example payload):

```http
POST http://master:9000/agents/register
{
    "id": "agent-pc2",
    "address": "10.0.0.2:9000",
    "specs": { "cpu_cores": 8, "ram_gb": 16 }
}
```

- Agent responsibilities:
  - Handle `POST /spawn` to start a worker process on a given port and return its address.
  - Handle `POST /kill` to terminate a worker process by ID.
  - Report machine CPU%, RAM%, and disk% to Master every 5s.
  - Store no persistent data.

### 5. Shared RAG module

- FAISS vector index and sentence-transformers embedder pre-loaded and shared for read-only access by workers to avoid loading heavy models multiple times.

### 6. LLM inference (mock)

- Mock implementation uses:

```py
asyncio.sleep(random.uniform(0.3, 1.2))
```

- Pluggable: swap with a real inference service (e.g., Ollama) without changing routing or orchestration.

## Databases

| DB           | Writer  | Purpose                                   |
| ------------ | ------- | ----------------------------------------- |
| Metrics DB   | Master  | Latency, throughput, worker stats, audit  |
| Request Logs | LB      | ReqID, worker that served it, response    |
| App DB       | Workers | RAG knowledge base, user data (read-only) |

All writes are asynchronous and never in the hot path.

## Communication

| From        | To     | Protocol | Purpose                                  |
| ----------- | ------ | -------- | ---------------------------------------- |
| Client      | LB     | HTTP     | External requests                        |
| LB          | Worker | gRPC     | Persistent, multiplexed hot-path calls   |
| Worker      | LB     | gRPC     | Responses (piggyback queue_depth)        |
| Master      | Agent  | HTTP     | Control commands (spawn/kill)            |
| Agent       | Master | HTTP     | Registration, periodic metrics (5s)      |
| Worker      | Master | HTTP     | Registration on startup                  |
| Master ↔ LB | —      | internal | Same-process function calls (no network) |

## Failure detection

gRPC keepalive and connection errors are used instead of a separate heartbeat:

- Active workers: a gRPC request error indicates the worker is dead (detected instantly).
- Idle workers: gRPC keepalive pings idle connections every 5s with a 3s timeout; no response → dead (max ~8s detection window).

Example Go keepalive configuration:

```go
grpc.WithKeepalive(keepalive.ClientParameters{
    Time:    5 * time.Second,
    Timeout: 3 * time.Second,
})
```

## Fault tolerance flow

### Worker dies (active)

1. gRPC request returns an error (t=0).
2. LB marks worker dead instantly and retrieves `in_flight_requests`.
3. LB retries those requests on alive workers immediately.
4. Master is notified and removes the worker from the registry; Master instructs Agent to clean up.

### Worker dies (idle)

1. Worker dies silently.
2. gRPC keepalive fails (ping at t=5s, timeout by ~t=8s).
3. Same recovery flow as active failure.

No requests are lost: active failures are recovered instantly; idle failures within ~8s.

## Auto-scaling

Master monitors system state every 5s (agent metrics + LB queue depths).

Scale-up trigger:

- Avg queue depth per worker > 10 OR any machine CPU > 80% → Master selects the agent with most free resources and posts a spawn command.

Scale-down trigger:

- Avg queue depth < 2 AND all machines CPU < 30% → Master selects a least-busy worker, instructs LB to drain it, waits for in-flight requests to finish, then tells Agent to kill it.

Scale actions are graceful: LB drains workers before termination to avoid dropped requests.

## Worker identity

- Worker ID format: `worker-{port}-{startup_timestamp}`
- Example: `worker-8001-1714123456`
- IDs are unique across restarts; Master and LB track by ID, not address.

## Startup sequence

1. Start the DB (Postgres via Docker or local).
2. On master host: `./start-master` — Master starts and waits for agent registrations; LB starts and waits for worker registry entries.
3. On other machines: `python agent.py --master http://10.0.0.1:9000` — each agent registers with Master and reports specs.
4. Master auto-spawns initial workers via agents; workers register and LB pool fills.

## Key design principles

- Master is the only controller and single source of truth.
- LB and Agents never make cluster-level decisions; they only execute.
- All hot-path writes are asynchronous.
- Workers are stateless and interchangeable.
- LB owns queue depth accounting; workers piggyback measurements to correct drift.
- gRPC persistent connections are used for failure detection (no separate heartbeat).
- Agents are dumb and stateless — losing an agent causes no data loss.

Here, you can find the documentation for the agent component of our system. The agent is responsible for managing local resources and communicating with the master component to coordinate task execution.

the agent simply is loadbalancer hands on other device where the loadbalancer has no control on it to make a new worker process and so on

the language didn't specified

the tasks of the agents are so simple
- monitor system resources
- add worker and return a way of communication (service_name or ip and port) smth to the loadbalancer so we can communicate

# Agent Component

The agent is a lightweight HTTP server that runs on worker nodes. It manages local system resources and worker containers, and registers itself with the master/load-balancer for task coordination.

## Endpoints

### GET `/system/info`

Returns host system resource information.

**Response `200 OK`**
```json
{
  "os": "linux",
  "cpu_usage": 45.2,
  "memory_mb": 15872
}
```

| Field | Type | Description |
|---|---|---|
| `os` | string | Go runtime OS name (e.g. `linux`, `darwin`) |
| `cpu_usage` | float64 | Total CPU usage percentage |
| `memory_mb` | uint64 | Available memory in MB |

---

### POST `/workers/create`

Creates a new worker Docker container and returns its connection info.

**Request**
```json
{
  "image": "llm-worker:latest",
  "name": "my-worker",
  "env": ["FOO=bar", "BAZ=qux"]
}
```

| Field | Type | Required | Description |
|---|---|---|---|
| `image` | string | no | Docker image (defaults to `--worker-image` flag or `llm-worker:latest`) |
| `name` | string | no | Container name (auto-generated if empty) |
| `env` | []string | no | Additional environment variables |

**Response `200 OK`**
```json
{
  "worker_id": "worker-agent-192.168.1.10-8080-50051-1712345678000",
  "address": "192.168.1.10:50051",
  "host_port": 50051,
  "container_port": 50051,
  "container_id": "abc123def456..."
}
```

| Field | Type | Description |
|---|---|---|
| `worker_id` | string | Unique worker identifier |
| `address` | string | `<host>:<port>` for gRPC communication |
| `host_port` | int | Host-side mapped port |
| `container_port` | int | Container-side gRPC port (always `50051`) |
| `container_id` | string | Docker container ID |

**Container details:**
- Exposes port `50051/tcp` bound to `0.0.0.0` on an auto-allocated host port (range: `--worker-port-start` to `--worker-port-end`, default `50051-50150`)
- Labels: `llm.cluster.role`, `llm.cluster.agent_id`, `llm.cluster.worker_id`, `llm.cluster.host_port`
- Restart policy: `unless-stopped`
- Auto-injected env vars: `WORKER_ID`, `WORKER_PORT`

**Errors:** `400` (bad request), `405` (method not allowed), `500` (Docker failure)

---

### Outbound: POST `<master-url>/agents/register`

Called automatically at agent startup (if `--master-url` is set). Registers the agent with the master so it can receive task assignments.

**Request**
```json
{
  "agent_id": "agent-192.168.1.10-8080",
  "address": "192.168.1.10:8080",
  "host": "192.168.1.10",
  "port": 8080
}
```

| Field | Type | Description |
|---|---|---|
| `agent_id` | string | Auto-generated agent identifier |
| `address` | string | Advertised `<host>:<port>` for agent HTTP API |
| `host` | string | LAN IP reachable by the master |
| `port` | int | HTTP port reachable by the master |

**Timeout:** 5 seconds

---

## CLI Flags

| Flag | Default | Description |
|---|---|---|
| `--listen` | `:9000` | Agent HTTP listen address |
| `--advertise-host` | auto-detected | LAN IP the master can use to reach this agent |
| `--advertise-port` | from `--listen` | LAN port the master can use to reach this agent |
| `--master-url` | `""` | Master HTTP base URL (enables registration) |
| `--worker-image` | `llm-worker:latest` | Default Docker image for workers |
| `--worker-port-start` | `50051` | Start of host port range for worker gRPC |
| `--worker-port-end` | `50150` | End of host port range for worker gRPC |
| `--ollama-url` | `http://127.0.0.1:11434` | Shared Ollama base URL passed to workers |
| `--chroma-url` | `http://127.0.0.1:8000` | Shared Chroma base URL passed to workers |

## Responsibilities

1. **Monitor system resources** — report CPU, memory, and OS info via `/system/info`
2. **Manage worker containers** — spin up Docker workers and return their gRPC address to the master via `/workers/create`
3. **Register with the master** — announce availability on startup so the master can route tasks to this agent
4. **Start shared Ollama** — if `--ollama-url` is set and Ollama is not running, the agent will start it locally
5. **Start shared Chroma** — if `--chroma-url` is set and Chroma is not running, the agent will start it via Docker compose

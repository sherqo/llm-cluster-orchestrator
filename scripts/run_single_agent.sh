#!/usr/bin/env bash
set -euo pipefail

REPO="${REPO:-$HOME/ac/uni/llm-cluster-orchestrator}"
AGENT_PORT="${1:-9000}"
OLLAMA_PORT="${2:-11434}"

# Handle MASTER_URL - use default if not set
if [[ -z "${MASTER_URL:-}" ]]; then
    MASTER_URL="http://localhost:8080"
fi
if [[ "$MASTER_URL" != http://* && "$MASTER_URL" != https://* ]]; then
    MASTER_URL="http://${MASTER_URL}"
fi

HOST_IP="${HOST_IP:-100.100.1.1}"

# Worker port range (optional, defaults)
WORKER_PORT_START="${3:-50051}"
WORKER_PORT_END="${4:-50150}"

echo "==> Starting agent on port $AGENT_PORT -> Ollama $OLLAMA_PORT"
echo "    master: $MASTER_URL"
echo "    worker ports: $WORKER_PORT_START-$WORKER_PORT_END"

cd "$REPO/agent"

exec go run . \
  --listen ":${AGENT_PORT}" \
  --advertise-host "$HOST_IP" \
  --advertise-port "$AGENT_PORT" \
  --master-url "$MASTER_URL" \
  --ollama-url "http://127.0.0.1:${OLLAMA_PORT}" \
  --chroma-url "http://127.0.0.1:8000" \
  --worker-port-start "$WORKER_PORT_START" \
  --worker-port-end "$WORKER_PORT_END"
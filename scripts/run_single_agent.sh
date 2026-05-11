#!/usr/bin/env bash
set -euo pipefail

REPO="${REPO:-$HOME/ac/uni/llm-cluster-orchestrator}"
AGENT_PORT="${1:-9000}"
OLLAMA_PORT="${2:-11434}"
if [[ "$MASTER_URL" != http://* && "$MASTER_URL" != https://* ]]; then
    MASTER_URL="http://${MASTER_URL}"
fi

HOST_IP="${HOST_IP:-100.100.1.1}"

echo "==> Starting agent on port $AGENT_PORT -> Ollama $OLLAMA_PORT"
echo "    master: $MASTER_URL"

cd "$REPO/agent"

exec go run . \
  --listen ":${AGENT_PORT}" \
  --advertise-host "$HOST_IP" \
  --advertise-port "$AGENT_PORT" \
  --master-url "$MASTER_URL" \
  --ollama-url "http://127.0.0.1:${OLLAMA_PORT}" \
  --chroma-url "http://127.0.0.1:8000"
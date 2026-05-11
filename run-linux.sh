#!/usr/bin/env bash
set -euo pipefail
REPO="${REPO:-$HOME/ac/uni/llm-cluster-orchestrator}"
MASTER_URL="${MASTER_URL:-http://100.100.1.2:8080}"
HOST_IP="${HOST_IP:-100.100.1.1}"
# Stop any existing ollama so each agent can start its own
pkill ollama || true
cd "$REPO/agent"
start_agent() {
  local idx="$1"
  local listen_port="$2"
  local worker_start="$3"
  local worker_end="$4"
  local ollama_port="$5"
  nohup go run . \
    --listen ":${listen_port}" \
    --advertise-host "$HOST_IP" \
    --advertise-port "${listen_port}" \
    --master-url "$MASTER_URL" \
    --worker-port-start "${worker_start}" \
    --worker-port-end "${worker_end}" \
    --ollama-url "http://127.0.0.1:${ollama_port}" \
    --chroma-url "http://127.0.0.1:8000" \
    > "$REPO/agent/agent-${idx}.log" 2>&1 &
}
start_agent 1 9000 50051 50090 11434
start_agent 2 9001 50091 50130 11435
start_agent 3 9002 50131 50170 11436
start_agent 4 9003 50171 50210 11437
echo "Started 4 agents. Logs in $REPO/agent/agent-*.log"
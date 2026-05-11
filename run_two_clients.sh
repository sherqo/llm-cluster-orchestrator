#!/bin/bash

MASTER_URL="${MASTER_URL:-http://localhost:8080}"

echo "Running two loadtest clients in parallel hitting $MASTER_URL"
echo "Each client auto-retries failed requests up to 5 times"
echo ""

# Client 1 - rate mode, continuous high throughput
go run ./client/loadtest/main.go \
    -master-url "$MASTER_URL" \
    -user-id "load-1" \
    -rate 100 \
    -rate-every 100ms \
    -burst 500 \
    -free-percent 50 \
    -paid-tier "pro" &

PID1=$!

# Client 2 - rate mode, continuous
go run ./client/loadtest/main.go \
    -master-url "$MASTER_URL" \
    -user-id "load-2" \
    -rate 100 \
    -rate-every 100ms \
    -burst 500 \
    -free-percent 50 \
    -paid-tier "elite" &

PID2=$!

echo "Started clients: $PID1 and $PID2"
echo "Press Ctrl+C to stop both"
echo ""
echo "Once running, press 'r' in each client to start rate mode, 'b' for bursts"
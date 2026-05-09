#!/bin/bash

URL="http://127.0.0.1:8080/chat"
COUNTER=0

phase() {
  local name="$1"
  local count="$2"
  local pause_after="$3"

  echo ""
  echo "═══════════════════════════════════════════════════════════════"
  echo "  Phase: $name"
  echo "  Sending $count requests in parallel..."
  echo "═══════════════════════════════════════════════════════════════"
  echo ""

  for j in $(seq 1 "$count"); do
    COUNTER=$((COUNTER + 1))
    if (( COUNTER % 5 == 0 )); then
        TIER="pro"
    else
        TIER="free"
    fi
    curl -s -X POST "$URL" \
      -H "Content-Type: application/json" \
      -d "{\"userId\":\"user-$COUNTER\",\"prompt\":\"Tell me a detailed story about the number $COUNTER\",\"tier\":\"$TIER\"}" \
      -o /dev/null -w "  [$(date +%H:%M:%S)] req $COUNTER ($TIER) HTTP %{http_code} in %{time_total}s\n" &
  done

  echo ""
  echo "  ⏳ Waiting for all $count requests to finish..."
  wait
  echo "  ✅ Phase '$name' completed."

  if [ "$pause_after" != "0" ] && [ "$pause_after" != "" ]; then
    echo "  💤 Cooling down for ${pause_after}s..."
    sleep "$pause_after"
  fi
}

START_TIME=$(date +%s)

echo ""
echo "▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓"
echo "  GRADUAL AUTOSCALER STRESS TEST"
echo "  Press Ctrl+C in the TUI to watch autoscaler behavior live"
echo "▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓"

# Phase 1: Small burst — triggers initial scale-up, then long cooldown
phase "75 small burst"  75  65

# Phase 2: Medium burst — proportional scaling, tests scale-up step limit
phase "150 medium burst" 150 40

# Phase 3: Large burst — should saturate max workers
phase "300 large burst"  300 30

# Phase 4: Tiny tickle — tests whether it holds workers or scales down
phase "50 tickle"         50  20

# Phase 5: Max burst — tests ceiling, queuing, and recovery
phase "500 max burst"    500  0

# Final wait to observe scale-down settling
echo ""
echo "  💤 Observing scale-down settling for 60s..."
sleep 60

END_TIME=$(date +%s)
DURATION=$((END_TIME - START_TIME))

echo ""
echo "═══════════════════════════════════════════════════════════════"
echo "  ✅ Gradual stress test finished in ${DURATION}s"
echo "  Total requests sent: $COUNTER"
echo "═══════════════════════════════════════════════════════════════"
echo ""
echo "  Tip: Replay the TUI log or check autoscaler metrics to"
echo "  evaluate response time, oscillation, and overscaling."
echo ""

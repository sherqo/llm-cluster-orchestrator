#!/usr/bin/env bash
set -euo pipefail
MASTER_URL="${MASTER_URL:-http://100.100.1.2:8080}"
USERS="${USERS:-1000}"
INTERVAL="${INTERVAL:-10}"
payload() {
  local user="$1"
  cat <<EOF
{"userId":"user-$user","prompt":"ping from user-$user","tier":"free"}
EOF
}
echo "Target: $MASTER_URL"
echo "Users: $USERS | Interval: ${INTERVAL}s"
echo "Press Ctrl+C to stop."
for i in $(seq 1 "$USERS"); do
  (
    while true; do
      curl -s -o /dev/null -X POST "$MASTER_URL/chat" \
        -H "Content-Type: application/json" \
        -d "$(payload "$i")"
      sleep "$INTERVAL"
    done
  ) &
done
wait
Run it:
chmod +x load_1000.sh
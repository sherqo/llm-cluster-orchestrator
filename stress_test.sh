#!/bin/bash

# Configuration
URL="http://127.0.0.1:8080/chat"
NUM_REQUESTS=1000

echo "🔥 Starting stress test: Sending $NUM_REQUESTS parallel requests to $URL"
echo "--------------------------------------------------------------------------------"

# Start time
START_TIME=$(date +%s)

# Send requests in parallel
for i in $(seq 1 $NUM_REQUESTS); do
  # Determine tier (mix of pro and free)
  if (( i % 5 == 0 )); then
      TIER="pro"
  else
      TIER="free"
  fi

  # Run curl in the background (&)
  curl -s -X POST "$URL" \
    -H "Content-Type: application/json" \
    -d "{\"userId\":\"user-$i\",\"prompt\":\"Tell me a long and detailed story about the number $i\",\"tier\":\"$TIER\"}" \
    -o /dev/null -w "Request $i ($TIER) completed with HTTP status: %{http_code} in %{time_total}s\n" &
done

echo "🚀 All $NUM_REQUESTS requests have been dispatched to the master node!"
echo "⏳ Waiting for the worker queue to clear and all responses to return..."
echo "--------------------------------------------------------------------------------"

# Wait for all background jobs to finish
wait

END_TIME=$(date +%s)
DURATION=$((END_TIME - START_TIME))

echo "--------------------------------------------------------------------------------"
echo "✅ Stress test finished in $DURATION seconds."

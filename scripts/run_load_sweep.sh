#!/usr/bin/env bash
set -euo pipefail

# Usage: scripts/run_load_sweep.sh <output_prefix>
# Example: scripts/run_load_sweep.sh results/run1

OUT_PREFIX="${1:-loadtest}"
OUT_DIR="$(dirname "${OUT_PREFIX}")"
MASTER_URL="${MASTER_URL:-http://localhost:8080}"
FREE_PERCENT="${FREE_PERCENT:-70}"
PAID_TIER="${PAID_TIER:-pro}"
CONCURRENCY="${CONCURRENCY:-50}"
TIMEOUT_SECONDS="${TIMEOUT_SECONDS:-600}"

if [ "$OUT_DIR" != "." ] && [ ! -d "$OUT_DIR" ]; then
  mkdir -p "$OUT_DIR"
fi

COUNTS=(10 100 500 1000 10000)

run_one() {
  local count="$1"
  local out_file="${OUT_PREFIX}_${count}.json"
  echo "==> Running load test: ${count} requests"
  echo "    Output: ${out_file}"
  python3 - <<'PY' "$MASTER_URL" "$FREE_PERCENT" "$PAID_TIER" "$CONCURRENCY" "$TIMEOUT_SECONDS" "$count" "$out_file"
import json
import random
import sys
import time
import urllib.request
from concurrent.futures import ThreadPoolExecutor, as_completed

master_url = sys.argv[1].rstrip("/")
free_percent = int(sys.argv[2])
paid_tier = sys.argv[3]
concurrency = int(sys.argv[4])
timeout_seconds = float(sys.argv[5])
count = int(sys.argv[6])
out_file = sys.argv[7]

prompts = [
    "Hello, how are you?",
    "Tell me something interesting about today.",
    "What is a good way to relax?",
    "Suggest a simple meal idea.",
    "How do I ask for directions politely?",
    "Give me a short travel tip.",
    "What can I do if I feel tired?",
    "Recommend a movie genre.",
    "What is a good question to start a conversation?",
]

def pick_tier(i):
    return "free" if (i % 100) < free_percent else paid_tier

def send_one(i):
    tier = pick_tier(i)
    payload = {
        "userId": f"load-user-{tier}-{i}",
        "prompt": prompts[i % len(prompts)],
        "tier": tier,
    }
    data = json.dumps(payload).encode("utf-8")
    req = urllib.request.Request(
        master_url + "/chat",
        data=data,
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    start = time.time()
    try:
        with urllib.request.urlopen(req, timeout=timeout_seconds) as resp:
            body = resp.read()
            ok = (resp.status == 200)
            return ok, time.time() - start, resp.status, body.decode("utf-8", errors="ignore")
    except Exception as exc:
        return False, time.time() - start, 0, str(exc)

start_time = time.time()
latencies = []
ok = 0
fail = 0
in_flight_max = 0

with ThreadPoolExecutor(max_workers=concurrency) as pool:
    futures = []
    for i in range(count):
        futures.append(pool.submit(send_one, i))
        in_flight_max = max(in_flight_max, len(futures))

    for fut in as_completed(futures):
        success, latency, _, _ = fut.result()
        latencies.append(latency)
        if success:
            ok += 1
        else:
            fail += 1

end_time = time.time()
duration = end_time - start_time

latencies.sort()
def percentile(p):
    if not latencies:
        return 0.0
    idx = int(round((len(latencies) - 1) * p))
    idx = max(0, min(idx, len(latencies) - 1))
    return latencies[idx]

lat_avg = sum(latencies) / len(latencies) if latencies else 0.0

report = {
    "StartTime": time.strftime("%Y-%m-%dT%H:%M:%S", time.localtime(start_time)),
    "EndTime": time.strftime("%Y-%m-%dT%H:%M:%S", time.localtime(end_time)),
    "DurationSeconds": duration,
    "RequestsSent": count,
    "RequestsOK": ok,
    "RequestsFailed": fail,
    "InFlightMax": in_flight_max,
    "Latency": {
        "Count": len(latencies),
        "MinMs": (latencies[0] * 1000.0) if latencies else 0.0,
        "MaxMs": (latencies[-1] * 1000.0) if latencies else 0.0,
        "AvgMs": lat_avg * 1000.0,
        "P50Ms": percentile(0.50) * 1000.0,
        "P95Ms": percentile(0.95) * 1000.0,
        "P99Ms": percentile(0.99) * 1000.0,
    },
    "Rate": {
        "CompletedPerSecOverall": (ok + fail) / duration if duration > 0 else 0.0,
        "SentPerSecOverall": count / duration if duration > 0 else 0.0,
    },
    "FreePct": free_percent,
    "PaidTier": paid_tier,
    "Concurrency": concurrency,
}

with open(out_file, "w", encoding="utf-8") as f:
    json.dump(report, f, indent=2)

print(json.dumps(report, indent=2))
PY
  echo "==> Finished ${count} requests"
}

for c in "${COUNTS[@]}"; do
  run_one "$c"
done

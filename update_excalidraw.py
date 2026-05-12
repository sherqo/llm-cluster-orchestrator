import json

replacements = {
    "MktGnyFJ": "Each worker has its own queue (in-memory)\nPiggybacks queue_depth on every response\nNo separate heartbeat (gRPC keepalive)",
    "XpiKqA9o": "gRPC keepalive every 5s",
    "q0S2zB8Y": "Instant death on gRPC error\nKeepalive timeout -> dead in ~8s",
    "adb4aTHn": "t=0s  gRPC error -> LB marks dead instantly",
    "xMVsUPAO": "t=0s  LB retrieves in_flight_requests",
    "vOBzIANS": "t=0s  LB retries on Worker 1 or 2 immediately",
    "Hu37xW6P": "Master removes worker & tells Agent to clean up",
    "Wnc9gDOU": "Idle workers detected in ~8s via keepalive",
    "F1bSCd4x": "No requests are lost",
    "sRgPeXtq": "Client gets response with minimal delay",
    "xY5HdEzR": "Unique ID = worker-{port}-{timestamp}\nWorkers stateless & safe to kill",
    "UBIep5Qi": "Triggers task requeue on death\nMonitors LB queue & CPU to Auto-scale",
}

with open("docs/system.excalidraw", "r") as f:
    data = json.load(f)

for el in data.get("elements", []):
    if el.get("type") == "text" and el.get("id") in replacements:
        new_text = replacements[el["id"]]
        el["text"] = new_text
        el["originalText"] = new_text

with open("docs/arch.excalidraw", "w") as f:
    json.dump(data, f, indent=2)

print("Done creating docs/arch.excalidraw")

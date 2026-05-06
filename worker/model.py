"""
model.py - real Ollama LLM runner.

This version gives each worker its own hard-coded Ollama server URL.
Example:
  worker 50051 -> Ollama http://127.0.0.1:11435
  worker 50052 -> Ollama http://127.0.0.1:11436
  worker 50053 -> Ollama http://127.0.0.1:11437

The worker starts its matching Ollama server in worker/main.py.
"""

import os
import requests


OLLAMA_MODEL = os.getenv("OLLAMA_MODEL", "llama3.2")
OLLAMA_TIMEOUT_SECONDS = int(os.getenv("OLLAMA_TIMEOUT_SECONDS", "180"))

WORKER_TO_OLLAMA_PORT = {
    50051: 11435,
    50052: 11436,
    50053: 11437,
    50054: 11438,
}


def get_ollama_port(worker_port: int) -> int:
    if worker_port in WORKER_TO_OLLAMA_PORT:
        return WORKER_TO_OLLAMA_PORT[worker_port]

    return 11435 + max(0, worker_port - 50051)


def get_ollama_url(worker_port: int) -> str:
    return f"http://127.0.0.1:{get_ollama_port(worker_port)}"


def build_prompt(prompt: str, context: str) -> str:
    if context:
        return (
            "You are a helpful assistant for a RAG system.\n"
            "Use the retrieved context when it is relevant.\n"
            "If the context is not enough, say what is missing and answer clearly.\n\n"
            f"Retrieved context:\n{context}\n\n"
            f"User question:\n{prompt}\n\n"
            "Answer:"
        )

    return (
        "You are a helpful assistant.\n\n"
        f"User question:\n{prompt}\n\n"
        "Answer:"
    )


def run_model(prompt: str, context: str, worker_port: int) -> str:
    ollama_url = get_ollama_url(worker_port)
    full_prompt = build_prompt(prompt, context)

    try:
        response = requests.post(
            f"{ollama_url}/api/generate",
            json={
                "model": OLLAMA_MODEL,
                "prompt": full_prompt,
                "stream": False,
                "options": {
                    "temperature": 0.2,
                },
            },
            timeout=OLLAMA_TIMEOUT_SECONDS,
        )
        response.raise_for_status()

        answer = response.json().get("response", "").strip()
        if not answer:
            return f"[worker:{worker_port}] Ollama returned an empty response."

        return (
            f"[worker:{worker_port}]\n"
            f"[ollama:{ollama_url}]\n"
            f"[model:{OLLAMA_MODEL}]\n\n"
            f"{answer}"
        )

    except requests.exceptions.ConnectionError:
        return (
            f"[worker:{worker_port}] ERROR: Could not connect to private Ollama at {ollama_url}.\n"
            "The worker should have started its own Ollama process.\n"
            "Check the worker terminal and the logs/ollama_*.log file."
        )

    except requests.exceptions.Timeout:
        return (
            f"[worker:{worker_port}] ERROR: Ollama timed out after "
            f"{OLLAMA_TIMEOUT_SECONDS} seconds. Try a smaller model such as llama3.2:1b."
        )

    except requests.exceptions.HTTPError as exc:
        return (
            f"[worker:{worker_port}] ERROR: Ollama HTTP error: {exc}\n"
            f"Response body: {response.text}"
        )

    except Exception as exc:
        return f"[worker:{worker_port}] ERROR while calling Ollama: {exc}"
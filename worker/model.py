import os
import requests

OLLAMA_MODEL = os.getenv("OLLAMA_MODEL", "smollm:135m")
OLLAMA_TIMEOUT_SECONDS = int(os.getenv("OLLAMA_TIMEOUT_SECONDS", "120"))

WORKER_TO_OLLAMA_PORT = {
    50051: 11435,
    50052: 11436,
    50053: 11437,
}


def get_ollama_port(worker_port: int) -> int:
    return WORKER_TO_OLLAMA_PORT.get(worker_port, 11435)


def get_ollama_url(worker_port: int) -> str:
    return f"http://127.0.0.1:{get_ollama_port(worker_port)}"


def build_prompt(prompt: str, context: str) -> str:
    if context:
        return (
            f"Use this context to answer the question. Give a direct answer only.\n\n"
            f"Context: {context}\n\n"
            f"Question: {prompt}\n"
            f"Answer:"
        )
    return (
        f"Answer this question directly: {prompt}\n"
        f"Answer:"
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
                    "num_predict": 200,  # limit response length for speed
                },
            },
            timeout=OLLAMA_TIMEOUT_SECONDS,
        )
        response.raise_for_status()
        return response.json().get("response", "").strip()

    except requests.exceptions.ConnectionError:
        return f"Ollama not running at {ollama_url}"
    except requests.exceptions.Timeout:
        return f"Ollama timed out — try a smaller model"
    except requests.exceptions.HTTPError as exc:
        return f"Ollama error: {exc} — {response.text}"
    except Exception as exc:
        return f"Error: {exc}"
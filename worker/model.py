import os
import requests

OLLAMA_MODEL = os.getenv("OLLAMA_MODEL", "smollm:135m")
OLLAMA_TIMEOUT_SECONDS = int(os.getenv("OLLAMA_TIMEOUT_SECONDS", "120"))


def get_ollama_url(worker_port: int) -> str:
    ollama_url = os.getenv("OLLAMA_URL", "").strip()
    if ollama_url:
        return ollama_url
    return f"http://127.0.0.1:11434"


def build_prompt(prompt: str, context: str) -> str:
    if context:
        return (
            f"{prompt}\n"
            f"keep the answers short\n\n"
            f"context: {context}\n\n"
        )
    return (
        f"{prompt}\n"
        f"keep the answers short\n\n"
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
                    "num_predict": 100,
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

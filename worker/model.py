"""
model.py — LLM Model Runner
Currently runs a mock LLM that simulates inference time.

To switch to a real model, replace the body of run_model()
with an Ollama API call:

    import requests
    response = requests.post("http://localhost:11434/api/generate",
        json={"model": "llama3", "prompt": full_prompt, "stream": False})
    return response.json()["response"]
"""

import time
import random


def run_model(prompt: str, context: str, worker_port: int) -> str:
    """
    Takes the user prompt and RAG context,
    runs the LLM (or mock), returns the reply string.

    Args:
        prompt:      the user's original question
        context:     retrieved chunks from ChromaDB (can be empty)
        worker_port: used in mock reply so you can see which worker answered
    """

    # ── BUILD THE FULL PROMPT ──────────────────
    # This is what would be sent to the real LLM.
    # Context from RAG is injected here so the model
    # can use it to answer the question accurately.
    if context:
        full_prompt = (
            f"You are a helpful assistant. Use the following context to answer the question.\n\n"
            f"Context:\n{context}\n\n"
            f"Question: {prompt}\n"
            f"Answer:"
        )
    else:
        full_prompt = (
            f"You are a helpful assistant.\n\n"
            f"Question: {prompt}\n"
            f"Answer:"
        )

    # ── MOCK INFERENCE ─────────────────────────
    # Simulates the time a real LLM would take (0.3 to 1.2 seconds)
    time.sleep(random.uniform(0.3, 1.2))

    # ── MOCK RESPONSE ──────────────────────────
    if context:
        return (
            f"[worker:{worker_port}]\n\n"
            f"Question: {prompt}\n\n"
            f"Retrieved context:\n{context}\n\n"
            f"Answer: This is a mock response. "
            f"Replace run_model() in model.py with a real Ollama call to get real answers."
        )
    else:
        return (
            f"[worker:{worker_port}]\n\n"
            f"Question: {prompt}\n\n"
            f"Answer: No relevant context found in ChromaDB. "
            f"This is a mock response with no RAG context."
        )
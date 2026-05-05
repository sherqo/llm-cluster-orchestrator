"""
rag.py — RAG Module
Embeds the query using sentence-transformers then
searches ChromaDB for the most relevant document chunks.
"""

import requests
from sentence_transformers import SentenceTransformer

# ─────────────────────────────────────────────
# CONFIG
# ─────────────────────────────────────────────
CHROMA_URL      = "http://localhost:8000"
COLLECTION_NAME = "documents"
TOP_K           = 3

# Load the same embedding model used when seeding ChromaDB
# Must match what was used to embed the documents!
print("[rag] loading embedding model...")
embedder = SentenceTransformer("all-MiniLM-L6-v2")
print("[rag] embedding model loaded.")


# ─────────────────────────────────────────────
# Get collection ID once at startup
# ─────────────────────────────────────────────
def _get_collection_id() -> str:
    try:
        resp = requests.get(
            f"{CHROMA_URL}/api/v1/collections/{COLLECTION_NAME}",
            timeout=5
        )
        if resp.status_code == 200:
            col_id = resp.json()["id"]
            print(f"[rag] connected to ChromaDB — collection='{COLLECTION_NAME}' id={col_id}")
            return col_id
        else:
            print(f"[rag] WARNING: collection '{COLLECTION_NAME}' not found — RAG disabled")
            return None
    except Exception as e:
        print(f"[rag] WARNING: ChromaDB unreachable: {e}")
        return None

COLLECTION_ID = _get_collection_id()


# ─────────────────────────────────────────────
# RETRIEVE
# ─────────────────────────────────────────────
def retrieve(prompt: str, top_k: int = TOP_K) -> str:
    """
    Embeds the prompt locally, then queries ChromaDB
    with the embedding vector to find similar documents.
    """
    if not COLLECTION_ID:
        print("[rag] skipping — no collection available")
        return ""

    try:
        # Step 1: embed the query into a vector
        query_vector = embedder.encode([prompt])[0].tolist()

        # Step 2: send the vector to ChromaDB
        resp = requests.post(
            f"{CHROMA_URL}/api/v1/collections/{COLLECTION_ID}/query",
            json={
                "query_embeddings": [query_vector],  # vector, not text
                "n_results": top_k,
            },
            timeout=10,
        )

        if resp.status_code != 200:
            print(f"[rag] query failed: {resp.status_code} — {resp.text}")
            return ""

        chunks = resp.json().get("documents", [[]])[0]

        if not chunks:
            print("[rag] no relevant chunks found")
            return ""

        context = "\n\n".join(f"[{i+1}] {chunk}" for i, chunk in enumerate(chunks))
        print(f"[rag] retrieved {len(chunks)} chunks for: '{prompt[:60]}'")
        return context

    except Exception as e:
        print(f"[rag] error: {e}")
        return ""
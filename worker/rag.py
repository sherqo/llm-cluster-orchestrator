import os
import requests

CHROMA_URL = os.getenv("CHROMA_URL", "http://localhost:8000")
COLLECTION_NAME = os.getenv("CHROMA_COLLECTION", "documents")
TOP_K = int(os.getenv("RAG_TOP_K", "1"))
MAX_CHUNK_CHARS = int(os.getenv("RAG_MAX_CHARS", "50"))


def _load_documents():
    try:
        col_resp = requests.get(
            f"{CHROMA_URL}/api/v1/collections/{COLLECTION_NAME}",
            timeout=5,
        )
        if col_resp.status_code != 200:
            print("[rag] WARNING: collection not found - RAG disabled")
            return []

        col_id = col_resp.json()["id"]
        docs_resp = requests.post(
            f"{CHROMA_URL}/api/v1/collections/{col_id}/get",
            json={"include": ["documents"]},
            timeout=10,
        )
        if docs_resp.status_code != 200:
            return []

        docs = docs_resp.json().get("documents", [])
        print(f"[rag] loaded {len(docs)} docs from Chroma")
        return docs
    except Exception as e:
        print(f"[rag] WARNING: error loading documents: {e}")
        return []


_DOCUMENTS = _load_documents()

def retrieve(prompt: str, top_k: int = TOP_K) -> str:
    if not _DOCUMENTS:
        return ""

    prompt_lower = prompt.lower()
    words = [w for w in prompt_lower.split() if len(w) > 2]

    best_doc = ""
    best_score = 0
    for doc in _DOCUMENTS:
        doc_lower = doc.lower()
        score = sum(1 for w in words if w in doc_lower)
        if score > best_score:
            best_score = score
            best_doc = doc

    if best_score > 0:
        return best_doc[:MAX_CHUNK_CHARS]
    return ""

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


_DOCUMENTS = None

def _get_documents():
    global _DOCUMENTS
    if _DOCUMENTS is None:
        _DOCUMENTS = _load_documents()
    return _DOCUMENTS


def _score(doc: str, terms: list[str]) -> int:
    text = doc.lower()
    return sum(text.count(t) for t in terms if t)


def retrieve(prompt: str, top_k: int = TOP_K) -> str:
    docs = _get_documents()
    if not docs:
        return ""

    terms = [t for t in prompt.lower().split() if len(t) > 2]
    scored = sorted(
        ((doc, _score(doc, terms)) for doc in DOCUMENTS),
        key=lambda x: x[1],
        reverse=True,
    )

    top_docs = [doc[:MAX_CHUNK_CHARS] for doc, s in scored[:top_k] if s > 0]
    if not top_docs:
        top_docs = [doc[:MAX_CHUNK_CHARS] for doc in docs[:top_k]]

    context = "\n\n".join(f"[{i+1}] {doc}" for i, doc in enumerate(top_docs))
    return context

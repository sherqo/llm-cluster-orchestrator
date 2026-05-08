import os
os.environ["HF_HUB_DISABLE_SYMLINKS_WARNING"] = "1"
os.environ["TOKENIZERS_PARALLELISM"] = "false"

import numpy as np
import requests
from sentence_transformers import SentenceTransformer

CHROMA_URL = os.getenv("CHROMA_URL", "http://localhost:8000")
COLLECTION_NAME = os.getenv("CHROMA_COLLECTION", "documents")
TOP_K = int(os.getenv("RAG_TOP_K", "2"))
MAX_CHUNK_CHARS = int(os.getenv("RAG_MAX_CHARS", "150"))

print("[rag] loading embedding model...")
embedder = SentenceTransformer("paraphrase-MiniLM-L3-v2")
print("[rag] embedding model loaded.")


def _load_documents():
    try:
        col_resp = requests.get(
            f"{CHROMA_URL}/api/v1/collections/{COLLECTION_NAME}",
            timeout=5,
        )
        if col_resp.status_code != 200:
            print("[rag] WARNING: collection not found — RAG disabled")
            return None, None

        col_id = col_resp.json()["id"]
        print(f"[rag] connected — collection='{COLLECTION_NAME}' id={col_id}")

        docs_resp = requests.post(
            f"{CHROMA_URL}/api/v1/collections/{col_id}/get",
            json={"include": ["documents"]},
            timeout=10,
        )
        if docs_resp.status_code != 200:
            return None, None

        docs = docs_resp.json().get("documents", [])
        if not docs:
            print("[rag] WARNING: no documents found")
            return None, None

        print(f"[rag] pre-embedding {len(docs)} documents...")
        doc_embeddings = embedder.encode(docs, convert_to_numpy=True, batch_size=32)
        doc_embeddings = doc_embeddings / np.linalg.norm(doc_embeddings, axis=1, keepdims=True)
        print(f"[rag] ready — {len(docs)} documents embedded in memory")
        return docs, doc_embeddings

    except Exception as e:
        print(f"[rag] WARNING: error loading documents: {e}")
        return None, None


DOCUMENTS, DOC_EMBEDDINGS = _load_documents()


def retrieve(prompt: str, top_k: int = TOP_K) -> str:
    if DOCUMENTS is None or DOC_EMBEDDINGS is None:
        return ""

    try:
        query_vec = embedder.encode([prompt], convert_to_numpy=True)[0]
        query_vec = query_vec / np.linalg.norm(query_vec)

        scores = DOC_EMBEDDINGS @ query_vec
        top_indices = np.argsort(scores)[::-1][:top_k]

        top_docs = [DOCUMENTS[i][:MAX_CHUNK_CHARS] for i in top_indices]
        context = "\n\n".join(f"[{i+1}] {doc}" for i, doc in enumerate(top_docs))
        print(f"[rag] retrieved {len(top_docs)} chunks for: '{prompt[:60]}'")
        return context

    except Exception as e:
        print(f"[rag] error: {e}")
        return ""

import os
os.environ["HF_HUB_DISABLE_SYMLINKS_WARNING"] = "1"

import requests

CHROMA_URL      = "http://localhost:8000"
COLLECTION_NAME = "documents"
TOP_K           = 2
MAX_CHUNK_CHARS = 150

print("[rag] connecting to ChromaDB...")


def _get_collection_id() -> str:
    try:
        resp = requests.get(
            f"{CHROMA_URL}/api/v1/collections/{COLLECTION_NAME}",
            timeout=5
        )
        if resp.status_code == 200:
            col_id = resp.json()["id"]
            print(f"[rag] connected — collection='{COLLECTION_NAME}' id={col_id}")
            return col_id
        else:
            print(f"[rag] WARNING: collection not found — RAG disabled")
            return None
    except Exception as e:
        print(f"[rag] WARNING: ChromaDB unreachable: {e}")
        return None

COLLECTION_ID = _get_collection_id()


def retrieve(prompt: str, top_k: int = TOP_K) -> str:
    if not COLLECTION_ID:
        return ""

    try:
        resp = requests.post(
            f"{CHROMA_URL}/api/v1/collections/{COLLECTION_ID}/get",
            json={"include": ["documents"]},
            timeout=10,
        )
        if resp.status_code != 200:
            return ""

        all_docs = resp.json().get("documents", [])
        if not all_docs:
            return ""

        stopwords = {"a","an","the","is","are","was","were","what","why",
                     "how","when","where","who","in","on","of","to","and","or"}
        keywords = [w for w in prompt.lower().split()
                    if w not in stopwords and len(w) > 2]

        scored = []
        for doc in all_docs:
            score = sum(1 for kw in keywords if kw in doc.lower())
            if score > 0:
                scored.append((score, doc))

        scored.sort(reverse=True)
        top_docs = [doc[:MAX_CHUNK_CHARS] for _, doc in scored[:top_k]]

        if not top_docs:
            return ""

        context = "\n".join(f"[{i+1}] {doc}" for i, doc in enumerate(top_docs))
        print(f"[rag] retrieved {len(top_docs)} chunks for: '{prompt[:60]}'")
        return context

    except Exception as e:
        print(f"[rag] error: {e}")
        return ""
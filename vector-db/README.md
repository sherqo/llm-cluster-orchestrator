# Vector DB (Chroma)

Semantic search service using ChromaDB with ONNX embeddings (CPU).

## Quick Start

```bash
docker compose up -d --build
```

## Configuration

| Env Variable | Description | Default |
|--------------|-------------|---------|
| `INITIALIZE` | Clear DB and embed docs (`true`/`false`) | `true` |
| `CHROMA_DB_PATH` | Database storage path | `/data/db` |
| `DOCS_PATH` | Source documents path | `/data/docs` |
| `COLLECTION_NAME` | Chroma collection name | `documents` |

## Adding Documents

1. Add `.md` or `.txt` files to `embedded-docs/`
2. Set `INITIALIZE: "true"` in `docker-compose.yaml`
3. Restart: `docker compose up -d --build`

Keep files short for better retrieval.

## Querying

Inside container:
```bash
python /app/scripts/query.py --query "your question" -k 3
```

External access via Chroma HTTP API at `http://localhost:8000`
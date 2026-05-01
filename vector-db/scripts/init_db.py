import os
import chromadb

DB_PATH = os.getenv("CHROMA_DB_PATH", "/data/db")
DOCS_PATH = os.getenv("DOCS_PATH", "/data/docs")

client = chromadb.PersistentClient(path=DB_PATH)

collection = client.get_or_create_collection("documents")

documents = []
ids = []

for file in os.listdir(DOCS_PATH):
    with open(os.path.join(DOCS_PATH, file), "r") as f:
        print(f"Indexing document: {file.split('.')[0]}")
        documents.append(f.read())
        ids.append(file)

collection.add(
    documents=documents,
    ids=ids
)

print("Documents indexed:", len(documents))
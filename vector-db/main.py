import os
import shutil
import chromadb
from chromadb.config import Settings

DB_PATH = os.getenv("DB_PATH", "/data/db")
DOCS_PATH = os.getenv("DOCS_PAHTH", "/data/docs")


print("Database path:", DB_PATH)
print("Documents path:", DOCS_PATH)



# wipe existing database if exists
if os.path.exists(DB_PATH):
    print("Clearing existing database...")
    shutil.rmtree(DB_PATH)

os.makedirs(DB_PATH, exist_ok=True)

client = chromadb.Client(
    Settings(
        persist_directory=DB_PATH
    )
)

collection = client.get_or_create_collection("documents")

model = SentenceTransformer("all-MiniLM-L6-v2")

documents = []
ids = []
embeddings = []

files = os.listdir(DOCS_PATH)

for i, file in enumerate(tqdm(files)):
    path = os.path.join(DOCS_PATH, file)
    if not os.path.isfile(path):
        continue

    with open(path, "r", encoding="utf-8") as f:
        text = f.read()
    documents.append(text)
    ids.append(file)

print("Generating embeddings...")

embeddings = model.encode(documents).tolist()

print("Inserting into Chroma...")

collection.add(
    documents=documents,
    embeddings=embeddings,
    ids=ids
)

print("Initialization complete.")

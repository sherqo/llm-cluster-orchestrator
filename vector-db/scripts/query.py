import os
import argparse
import chromadb
from chromadb.config import Settings


def connect():
    db_path = os.getenv("CHROMA_DB_PATH", "/data/db")

    client = chromadb.PersistentClient(
        path=db_path,
        settings=Settings(anonymized_telemetry=False)
    )

    return client


def query(collection, text, k):
    results = collection.query(
        query_texts=[text],
        n_results=k
    )

    return results


def print_results(results):
    docs = results["documents"][0]
    ids = results["ids"][0]
    distances = results["distances"][0]

    for i, (doc, id_, dist) in enumerate(zip(docs, ids, distances)):
        print("\n---------------------------")
        print(f"Rank: {i+1}")
        print(f"ID: {id_}")
        print(f"Distance: {dist:.4f}")
        print("Snippet:")
        print(doc[:300])


def main():
    parser = argparse.ArgumentParser()

    parser.add_argument(
        "--query",
        required=True,
        help="Query text"
    )

    parser.add_argument(
        "-k",
        type=int,
        default=3,
        help="Number of results"
    )

    args = parser.parse_args()

    client = connect()
    collection = client.get_collection("documents")

    results = query(collection, args.query, args.k)

    print_results(results)


if __name__ == "__main__":
    main()
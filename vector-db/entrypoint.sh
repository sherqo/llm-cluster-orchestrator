#!/bin/bash

set -e

echo "Container starting..."

if [ "$INITIALIZE" = "true" ]; then
    echo "INITIALIZE=true detected"
    echo "Clearing existing database files..."
    mkdir -p /data/db
    rm -rf /data/db/*
    echo "Running database initialization..."
    python /app/scripts/init_db.py
    echo "Database initialization completed"

else
    echo "Skipping initialization"
fi

echo "Testing DB with sample query..."
if python /app/scripts/query.py --query "france" -k 3; then
    echo "Sample query completed successfully"
else
    echo "Sample query failed; continuing startup"
fi

echo "Starting Chroma server..."

exec chroma run --host 0.0.0.0 --port 8000 --path /data/db
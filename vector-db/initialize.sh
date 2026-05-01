#!/bin/bash

set -e

echo "Container starting..."

if [ "$INITIALIZE" = "true" ]; then
    echo "INITIALIZE=true detected"
    echo "Running database initialization..."
    python /app/scripts/init_db.py
else
    echo "Skipping initialization"
fi

echo "Starting Chroma server..."

exec chroma run --host 0.0.0.0 --port 8000 --path /data/db

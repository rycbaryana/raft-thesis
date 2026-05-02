#!/bin/bash

# Default log level is info. Usage: ./cluster.sh [debug|info|warn|error] [memory|disk]
LOG_LEVEL=${1:-info}
STORAGE=${2:-memory}

set -euo pipefail
cd "$(dirname "$0")"

./kill-node.sh 1 2 3 >/dev/null 2>&1 || true

echo "Building node..."
go build -o build/node cmd/node/main.go

echo "Starting node 1..."
./start-node.sh 1 "$LOG_LEVEL" "$STORAGE"
echo "Starting node 2..."
./start-node.sh 2 "$LOG_LEVEL" "$STORAGE"
echo "Starting node 3..."
./start-node.sh 3 "$LOG_LEVEL" "$STORAGE"

echo "Cluster started."
echo "Press Ctrl+C to stop all nodes"

# Handle Ctrl+C to kill all child processes
trap "./kill-node.sh 1 2 3; exit" INT TERM

# Tail logs of all nodes
tail -f logs/node1.log logs/node2.log logs/node3.log

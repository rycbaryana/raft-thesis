#!/usr/bin/env bash
# Start a single node (after kill or for ad-hoc tests). Requires ./node binary.
# Usage: ./start-node.sh <1|2|3> [log-level] [storage]
# Example: ./start-node.sh 2
#          ./start-node.sh 2 debug
#          ./start-node.sh 2 info disk

set -euo pipefail
cd "$(dirname "$0")"

ID="${1:-}"
LOG_LEVEL="${2:-info}"
STORAGE="${3:-memory}"

if [[ ! "$ID" =~ ^[123]$ ]]; then
	echo "Usage: $0 <1|2|3> [debug|info|warn|error] [memory|disk]" >&2
	exit 1
fi

PORT=$((8080 + ID))
if lsof -ti "tcp:${PORT}" -sTCP:LISTEN >/dev/null 2>&1; then
	echo "Port $PORT already in use; node $ID may already be running." >&2
	exit 1
fi

if [[ ! -x ./build/node ]]; then
	echo "Missing ./build/node — run: go build -o build/node cmd/node/main.go" >&2
	exit 1
fi

LOG="logs/node${ID}.log"
mkdir -p logs
echo "Starting node $ID (-log-level $LOG_LEVEL -storage $STORAGE), appending to $LOG"
./build/node -id "$ID" -log-level "$LOG_LEVEL" -storage "$STORAGE" -data-dir "./data" >>"$LOG" 2>&1 &
echo "Node $ID started PID $!"

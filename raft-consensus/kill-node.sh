#!/usr/bin/env bash
# Stop one or more nodes by ID (listener on localhost:8081..8083).
# Usage: ./kill-node.sh <1|2|3> [1|2|3 ...]
# Example: ./kill-node.sh 2        # kill follower/leader on 8082
#          ./kill-node.sh 2 3      # partition two nodes

set -euo pipefail
cd "$(dirname "$0")"

if [[ $# -lt 1 ]]; then
	echo "Usage: $0 <1|2|3> [1|2|3 ...]" >&2
	exit 1
fi

for ID in "$@"; do
	if [[ ! "$ID" =~ ^[123]$ ]]; then
		echo "Invalid node id: $ID (expected 1, 2, or 3)" >&2
		exit 1
	fi
	PORT=$((8080 + ID))
	PIDS=$(lsof -ti "tcp:${PORT}" -sTCP:LISTEN 2>/dev/null || true)
	if [[ -z "$PIDS" ]]; then
		echo "Node $ID: nothing listening on port $PORT (already stopped?)"
		continue
	fi
	for p in $PIDS; do
		echo "Node $ID: killing PID $p (port $PORT)"
		kill "$p" 2>/dev/null || kill -9 "$p" 2>/dev/null || true
	done
done

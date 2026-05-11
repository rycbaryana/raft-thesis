#!/usr/bin/env bash
# Stop one or more nodes by ID (listener on localhost:8080+id).
# Usage: ./kill-node.sh <node_id> [node_id ...]
# Example: ./kill-node.sh 2        # kill node on 8082
#          ./kill-node.sh 2 3 4

set -euo pipefail
_RAFTP_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" && pwd)"
# shellcheck disable=SC1091
source "$_RAFTP_DIR/raft-common.sh"
cd "$_RAFTP_DIR"

if [[ $# -lt 1 ]]; then
	echo "Usage: $0 <node_id> [node_id ...]" >&2
	exit 1
fi

for ID in "$@"; do
	if ! raft_valid_node_id "$ID"; then
		echo "Invalid node id: $ID (expected positive integer)" >&2
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

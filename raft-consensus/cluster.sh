#!/usr/bin/env bash
# Build and start a local cluster as the FOUNDING set, then tail logs.
#
# All nodes in RAFT_NODE_IDS are started with the same -bootstrap CSV, so
# they form one initial Raft configuration. To later add more nodes use
# ./start-node.sh <new_id> (no bootstrap) + ./add-node.sh.
#
# Usage: ./cluster.sh [debug|info|warn|error] [memory|disk]
#
# Which nodes to start/kill/tail: set RAFT_NODE_IDS (space-separated), default "1 2 3".
# Example: RAFT_NODE_IDS="1 2 4" ./cluster.sh debug memory

LOG_LEVEL=${1:-info}
STORAGE=${2:-memory}

set -euo pipefail
_RAFTP_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" && pwd)"
# shellcheck disable=SC1091
source "$_RAFTP_DIR/raft-common.sh"
cd "$_RAFTP_DIR"

for id in $RAFT_NODE_IDS; do
	if ! raft_valid_node_id "$id"; then
		echo "Invalid RAFT_NODE_IDS entry: $id" >&2
		exit 1
	fi
done

# Build the founding bootstrap CSV from RAFT_NODE_IDS.
BOOTSTRAP_CSV="$(echo "$RAFT_NODE_IDS" | tr -s ' ' ',' | sed 's/^,//;s/,$//')"

./kill-node.sh $RAFT_NODE_IDS >/dev/null 2>&1 || true

echo "Building node..."
go build -o build/node ./cmd/node

mkdir -p logs
rm -f logs/*.log

for id in $RAFT_NODE_IDS; do
	echo "Starting node ${id} (bootstrap=${BOOTSTRAP_CSV})..."
	./start-node.sh "$id" "$LOG_LEVEL" "$STORAGE" "$BOOTSTRAP_CSV"
done

echo "Cluster started (nodes: $RAFT_NODE_IDS)."
echo "Press Ctrl+C to stop all nodes"

trap './kill-node.sh $RAFT_NODE_IDS; exit' INT TERM

LOG_FILES=()
for id in $RAFT_NODE_IDS; do
	LOG_FILES+=("logs/node${id}.log")
done
tail -f "${LOG_FILES[@]}"

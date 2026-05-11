#!/usr/bin/env bash
# Start a single node (after kill or for ad-hoc tests). Requires ./build/node binary.
#
# Usage:
#   ./start-node.sh <node_id> [log-level] [storage] [bootstrap-csv]
#
# bootstrap-csv:
#   - Pass for the very first start of a founding member, e.g. "1,2,3".
#     All founding members must agree on the same set.
#   - OMIT (or pass empty) for a new node that joins an existing cluster.
#     Such a node sits idle until the operator runs ./add-node.sh on the leader.
#   - Can also be supplied via the BOOTSTRAP env var (positional arg wins).
#
# Examples:
#   ./start-node.sh 2 info memory "1,2,3"   # founding member
#   ./start-node.sh 4                       # joiner (no bootstrap)
#   BOOTSTRAP="1,2,3" ./start-node.sh 1     # founding via env var
#
# HTTP/RPC port: 8080 + node_id.

set -euo pipefail
_RAFTP_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" && pwd)"
# shellcheck disable=SC1091
source "$_RAFTP_DIR/raft-common.sh"
cd "$_RAFTP_DIR"

ID="${1:-}"
LOG_LEVEL="${2:-info}"
STORAGE="${3:-memory}"
BOOTSTRAP_ARG="${4:-${BOOTSTRAP:-}}"

if ! raft_valid_node_id "$ID"; then
	echo "Usage: $0 <node_id> [debug|info|warn|error] [memory|disk] [bootstrap-csv]" >&2
	echo "  node_id — положительное целое (порт localhost:\$((8080+id)))." >&2
	exit 1
fi

PORT=$((8080 + ID))
if lsof -ti "tcp:${PORT}" -sTCP:LISTEN >/dev/null 2>&1; then
	echo "Port $PORT already in use; node $ID may already be running." >&2
	exit 1
fi

if [[ ! -x ./build/node ]]; then
	echo "Missing ./build/node — run: go build -o build/node ./cmd/node" >&2
	exit 1
fi

LOG="logs/node${ID}.log"
mkdir -p logs

CMD=(./build/node -id "$ID" -log-level "$LOG_LEVEL" -storage "$STORAGE" -data-dir "./data")
if [[ -n "$BOOTSTRAP_ARG" ]]; then
	CMD+=(-bootstrap "$BOOTSTRAP_ARG")
	echo "Starting node $ID as FOUNDING member (bootstrap=$BOOTSTRAP_ARG, log-level=$LOG_LEVEL, storage=$STORAGE), appending to $LOG"
else
	echo "Starting node $ID as JOINER (no bootstrap; waits for AddNode, log-level=$LOG_LEVEL, storage=$STORAGE), appending to $LOG"
fi
"${CMD[@]}" >>"$LOG" 2>&1 &
echo "Node $ID started PID $!"

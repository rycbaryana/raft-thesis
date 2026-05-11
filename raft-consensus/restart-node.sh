#!/usr/bin/env bash
# Kill then start one node (chaos / recovery checks).
#
# Usage: ./restart-node.sh <node_id> [log-level] [storage] [bootstrap-csv]
#
# In memory-storage mode a restarted founding member loses all state, so you
# must re-supply its bootstrap CSV (e.g. "1,2,3"). For disk storage or for a
# previously-joined node, omit bootstrap.

set -euo pipefail
_RAFTP_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" && pwd)"
# shellcheck disable=SC1091
source "$_RAFTP_DIR/raft-common.sh"

ID="${1:-}"
LOG_LEVEL="${2:-info}"
STORAGE="${3:-memory}"
BOOTSTRAP_ARG="${4:-${BOOTSTRAP:-}}"

if ! raft_valid_node_id "$ID"; then
	echo "Usage: $0 <node_id> [debug|info|warn|error] [memory|disk] [bootstrap-csv]" >&2
	exit 1
fi

"$_RAFTP_DIR/kill-node.sh" "$ID" || true
sleep 0.3
"$_RAFTP_DIR/start-node.sh" "$ID" "$LOG_LEVEL" "$STORAGE" "$BOOTSTRAP_ARG"

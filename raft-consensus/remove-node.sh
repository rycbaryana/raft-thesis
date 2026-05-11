#!/usr/bin/env bash
# Remove a cluster member (Raft RemoveNode via HTTP DELETE), following leader hints.
#
# Usage:
#   ./remove-node.sh <node_id>              # auto-discover leader via RAFT_NODE_IDS
#   ./remove-node.sh <gateway_id> <node_id> # start with specific gateway
#
# RAFT_NODE_IDS="1 2 4" ./remove-node.sh 5

set -euo pipefail
_RAFTP_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" && pwd)"
# shellcheck disable=SC1091
source "$_RAFTP_DIR/raft-common.sh"
cd "$_RAFTP_DIR"

usage() {
	echo "Usage: $0 <node_id>              # auto-discover leader via RAFT_NODE_IDS" >&2
	echo "       $0 <gateway_id> <node_id> # start with specific gateway" >&2
	exit 1
}

[[ $# -lt 1 ]] && usage

raft_init

if [[ $# -eq 2 ]] && raft_valid_node_id "$1"; then
	NODE_ID=$1; REMOVE_ID=$2
elif [[ $# -eq 1 ]] && raft_valid_node_id "$1"; then
	NODE_ID=$(raft_first_node_id); REMOVE_ID=$1
else
	usage
fi

if ! raft_valid_node_id "$REMOVE_ID"; then
	echo "Invalid node id to remove: $REMOVE_ID" >&2
	exit 1
fi

do_remove() {
	curl -sS -X DELETE --dump-header "$_raft_hdr_file" \
		"$1/cluster/nodes?id=${REMOVE_ID}" \
		-w "\nHTTP_STATUS:%{http_code}" || true
}

echo "Removing node ${REMOVE_ID} → node ${NODE_ID} ($(raft_node_url "$NODE_ID"))..."
raft_request do_remove "$NODE_ID"

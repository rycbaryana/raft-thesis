#!/usr/bin/env bash
# Add a cluster member (Raft AddNode via HTTP), following leader hints.
#
# Usage:
#   ./add-node.sh <new_node_id>                         # addr = localhost:$((8080+id))
#   ./add-node.sh <new_node_id> <rpc_addr>              # custom addr
#   ./add-node.sh <gateway_id> <new_node_id>            # specific gateway, default addr
#   ./add-node.sh <gateway_id> <new_node_id> <rpc_addr>
#
# RAFT_NODE_IDS="1 2 4" ./add-node.sh 5

set -euo pipefail
_RAFTP_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" && pwd)"
# shellcheck disable=SC1091
source "$_RAFTP_DIR/raft-common.sh"
cd "$_RAFTP_DIR"

default_rpc_addr() {
	echo "localhost:$((8080 + $1))"
}

usage() {
	echo "Usage: $0 <new_node_id>                         # default addr" >&2
	echo "       $0 <new_node_id> <rpc_addr>              # custom addr" >&2
	echo "       $0 <gateway_id> <new_node_id>            # specific gateway" >&2
	echo "       $0 <gateway_id> <new_node_id> <rpc_addr>" >&2
	exit 1
}

[[ $# -lt 1 ]] && usage

raft_init

if [[ $# -eq 3 ]] && raft_valid_node_id "$1"; then
	NODE_ID=$1; NEW_ID=$2; ADDR=$3
elif [[ $# -eq 2 ]] && raft_valid_node_id "$1" && raft_valid_node_id "$2"; then
	NODE_ID=$1; NEW_ID=$2; ADDR=$(default_rpc_addr "$NEW_ID")
elif [[ $# -eq 1 ]] && raft_valid_node_id "$1"; then
	NODE_ID=$(raft_first_node_id); NEW_ID=$1; ADDR=$(default_rpc_addr "$NEW_ID")
elif [[ $# -eq 2 ]] && raft_valid_node_id "$1"; then
	NODE_ID=$(raft_first_node_id); NEW_ID=$1; ADDR=$2
else
	usage
fi

do_add() {
	curl -sS -X POST --dump-header "$_raft_hdr_file" \
		"$1/cluster/nodes" \
		-H 'Content-Type: application/json' \
		-d "{\"id\":${NEW_ID},\"addr\":\"${ADDR}\"}" \
		-w "\nHTTP_STATUS:%{http_code}" || true
}

echo "Adding node ${NEW_ID} (${ADDR}) → node ${NODE_ID} ($(raft_node_url "$NODE_ID"))..."
raft_request do_add "$NODE_ID"

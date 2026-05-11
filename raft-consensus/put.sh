#!/usr/bin/env bash
# PUT to the cluster, following leader hints via X-Raft-Leader-Id.
#
# Usage:
#   ./put.sh <key> <value>                    # starts with first node in RAFT_NODE_IDS
#   ./put.sh <gateway_id> <key> <value>       # starts with the specified node
#
# RAFT_NODE_IDS="1 2 4" ./put.sh k v

set -euo pipefail
_RAFTP_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" && pwd)"
# shellcheck disable=SC1091
source "$_RAFTP_DIR/raft-common.sh"
cd "$_RAFTP_DIR"

usage() {
	echo "Usage: $0 <key> <value>                    # auto-discover leader via RAFT_NODE_IDS" >&2
	echo "       $0 <gateway_id> <key> <value>       # start with specific node" >&2
	exit 1
}

[[ $# -lt 2 ]] && usage

raft_init

if [[ $# -eq 3 ]] && raft_valid_node_id "$1"; then
	NODE_ID=$1; KEY=$2; VAL=$3
elif [[ $# -eq 2 ]]; then
	NODE_ID=$(raft_first_node_id); KEY=$1; VAL=$2
else
	usage
fi

do_put() {
	curl -sS -G --dump-header "$_raft_hdr_file" \
		-w "\nHTTP_STATUS:%{http_code}" \
		--data-urlencode "key=${KEY}" \
		--data-urlencode "val=${VAL}" \
		"$1/put" || true
}

echo "PUT key='${KEY}' val='${VAL}' → node ${NODE_ID} ($(raft_node_url "$NODE_ID"))..."
raft_request do_put "$NODE_ID"

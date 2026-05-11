#!/usr/bin/env bash
# GET from the cluster, following leader hints via X-Raft-Leader-Id.
#
# Usage:
#   ./get.sh <key>                    # starts with first node in RAFT_NODE_IDS
#   ./get.sh <gateway_id> <key>       # starts with the specified node
#
# RAFT_NODE_IDS="1 2 4" ./get.sh mykey

set -euo pipefail
_RAFTP_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" && pwd)"
# shellcheck disable=SC1091
source "$_RAFTP_DIR/raft-common.sh"
cd "$_RAFTP_DIR"

usage() {
	echo "Usage: $0 <key>                    # auto-discover leader via RAFT_NODE_IDS" >&2
	echo "       $0 <gateway_id> <key>       # start with specific node" >&2
	exit 1
}

[[ $# -lt 1 ]] && usage

raft_init

if [[ $# -eq 2 ]] && raft_valid_node_id "$1"; then
	NODE_ID=$1; KEY=$2
elif [[ $# -eq 1 ]]; then
	NODE_ID=$(raft_first_node_id); KEY=$1
else
	usage
fi

do_get() {
	curl -sS -G --dump-header "$_raft_hdr_file" \
		-w "\nHTTP_STATUS:%{http_code}" \
		--data-urlencode "key=${KEY}" \
		"$1/get" || true
}

echo "GET key='${KEY}' → node ${NODE_ID} ($(raft_node_url "$NODE_ID"))..."
raft_request do_get "$NODE_ID"

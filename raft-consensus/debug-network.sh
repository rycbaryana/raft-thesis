#!/usr/bin/env bash
# Рубильник полной эмуляции разрыва Raft-сети на узле (исходящие + входящие RPC).
#
# Usage:
#   ./debug-network.sh on <node_id>     # isolated=true
#   ./debug-network.sh off <node_id>   # isolated=false
# Синонимы: disconnect/reconnect (как раньше)

set -euo pipefail
_RAFTP_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" && pwd)"
# shellcheck disable=SC1091
source "$_RAFTP_DIR/raft-common.sh"
cd "$_RAFTP_DIR"

usage() {
	echo "Usage: $0 on|off <node_id>" >&2
	echo "       $0 disconnect|reconnect <node_id>" >&2
	exit 1
}

if [[ $# -ne 2 ]]; then
	usage
fi

ACTION=$1
ID=$2

if ! raft_valid_node_id "$ID"; then
	echo "Invalid node id: $ID (expected positive integer)" >&2
	exit 1
fi

case "$ACTION" in
on | disconnect | isolate) ISOLATED=true ;;
off | reconnect | heal) ISOLATED=false ;;
*)
	echo "Unknown action: $ACTION" >&2
	usage
	;;
esac

PORT=$((8080 + ID))
BASE="http://localhost:${PORT}"
URL="${BASE}/debug/network/partition?isolated=${ISOLATED}"

RESPONSE=$(curl -sS -w "\nHTTP_STATUS:%{http_code}" -X POST "$URL" || true)
BODY=$(echo "$RESPONSE" | sed -e 's/HTTP_STATUS\:.*//g')
STATUS=$(echo "$RESPONSE" | tr -d '\n' | sed -e 's/.*HTTP_STATUS://')

if [[ "$STATUS" == "200" ]]; then
	echo "OK node $ID (partition isolated=${ISOLATED}): $BODY"
	exit 0
fi

echo "Node $ID: HTTP $STATUS — $BODY" >&2
exit 1

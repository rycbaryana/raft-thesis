#!/usr/bin/env bash
# Рубильник полной эмуляции разрыва Raft-сети на узле (исходящие + входящие RPC).
#
# Usage:
#   ./debug-network.sh on <1|2|3>     # isolated=true
#   ./debug-network.sh off <1|2|3>    # isolated=false
# Синонимы: disconnect/reconnect (как раньше)

set -euo pipefail
cd "$(dirname "$0")"

usage() {
	echo "Usage: $0 on|off <1|2|3>" >&2
	echo "       $0 disconnect|reconnect <1|2|3>  # same as on/off" >&2
	exit 1
}

if [[ $# -ne 2 ]]; then
	usage
fi

ACTION=$1
ID=$2

if [[ ! "$ID" =~ ^[123]$ ]]; then
	echo "Invalid node id: $ID (expected 1, 2, or 3)" >&2
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

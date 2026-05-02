#!/usr/bin/env bash
# PUT на кластер или на конкретный узел (HTTP).
#
# Usage:
#   ./put.sh <key> <value>              # перебор 8081..8083, пока лидер не примет
#   ./put.sh <1|2|3> <key> <value>      # только на узел с этим id (порт 8080+id)

set -euo pipefail
cd "$(dirname "$0")"

usage() {
	echo "Usage: $0 <key> <value>              # try all nodes" >&2
	echo "       $0 <1|2|3> <key> <value>     # single node only" >&2
	exit 1
}

node_url() {
	local id=$1
	echo "http://localhost:$((8080 + id))"
}

do_put() {
	local base=$1
	local key=$2
	local val=$3
	curl -sS -G -w "\nHTTP_STATUS:%{http_code}" \
		--data-urlencode "key=${key}" \
		--data-urlencode "val=${val}" \
		"${base}/put" || true
}

if [[ $# -lt 2 ]]; then
	usage
fi

if [[ $# -eq 3 && "$1" =~ ^[123]$ ]]; then
	NODE_ID=$1
	KEY=$2
	VAL=$3
	BASE=$(node_url "${NODE_ID}")
	echo "PUT to node ${NODE_ID} (${BASE})..."
	RESPONSE=$(do_put "${BASE}" "${KEY}" "${VAL}")
else
	if [[ $# -ne 2 ]]; then
		usage
	fi
	KEY=$1
	VAL=$2
	for NODE_ID in 1 2 3; do
		BASE=$(node_url "${NODE_ID}")
		echo "Trying ${BASE}..."
		RESPONSE=$(do_put "${BASE}" "${KEY}" "${VAL}")
		BODY=$(echo "${RESPONSE}" | sed -e 's/HTTP_STATUS\:.*//g')
		STATUS=$(echo "${RESPONSE}" | tr -d '\n' | sed -e 's/.*HTTP_STATUS://')
		if [[ "${STATUS}" == "200" ]]; then
			echo -e "\n✅ Success on ${BASE}: ${BODY}"
			exit 0
		fi
		echo "❌ ${BASE} replied with status ${STATUS}: ${BODY}"
	done
	echo -e "\nFailed to put '${KEY}=${VAL}'. No leader found or cluster is down."
	exit 1
fi

BODY=$(echo "${RESPONSE}" | sed -e 's/HTTP_STATUS\:.*//g')
STATUS=$(echo "${RESPONSE}" | tr -d '\n' | sed -e 's/.*HTTP_STATUS://')
if [[ "${STATUS}" == "200" ]]; then
	echo "✅ Success: ${BODY}"
	exit 0
fi
echo "❌ HTTP ${STATUS}: ${BODY}" >&2
exit 1

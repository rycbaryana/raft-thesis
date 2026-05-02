#!/usr/bin/env bash
# GET с кластера или с конкретного узла (HTTP).
#
# Usage:
#   ./get.sh <key>                 # перебор 8081..8083
#   ./get.sh <1|2|3> <key>         # только узел с этим id

set -euo pipefail
cd "$(dirname "$0")"

usage() {
	echo "Usage: $0 <key>                 # try all nodes" >&2
	echo "       $0 <1|2|3> <key>        # single node only" >&2
	exit 1
}

node_url() {
	local id=$1
	echo "http://localhost:$((8080 + id))"
}

do_get() {
	local base=$1
	local key=$2
	curl -sS -G -w "\nHTTP_STATUS:%{http_code}" \
		--data-urlencode "key=${key}" \
		"${base}/get" || true
}

if [[ $# -lt 1 ]]; then
	usage
fi

if [[ $# -eq 2 && "$1" =~ ^[123]$ ]]; then
	NODE_ID=$1
	KEY=$2
	BASE=$(node_url "${NODE_ID}")
	echo "GET from node ${NODE_ID} (${BASE})..."
	RESPONSE=$(do_get "${BASE}" "${KEY}")
else
	if [[ $# -ne 1 ]]; then
		usage
	fi
	KEY=$1
	for NODE_ID in 1 2 3; do
		BASE=$(node_url "${NODE_ID}")
		echo "Trying ${BASE}..."
		RESPONSE=$(do_get "${BASE}" "${KEY}")
		BODY=$(echo "${RESPONSE}" | sed -e 's/HTTP_STATUS\:.*//g')
		STATUS=$(echo "${RESPONSE}" | tr -d '\n' | sed -e 's/.*HTTP_STATUS://')
		if [[ "${STATUS}" == "200" ]]; then
			echo -e "\n✅ Success on ${BASE}: ${BODY}"
			exit 0
		fi
		echo "❌ ${BASE} replied with status ${STATUS}: ${BODY}"
	done
	echo -e "\nFailed to get '${KEY}'. No leader found or cluster is down."
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

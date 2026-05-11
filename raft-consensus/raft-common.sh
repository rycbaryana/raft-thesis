#!/usr/bin/env bash
# Shared helpers for raft-consensus shell scripts (source from same directory).
#
# Space-separated Raft node IDs used for gateway selection.
# Example: RAFT_NODE_IDS="1 2 4 5" ./put.sh k v
: "${RAFT_NODE_IDS:=1 2 3}"

raft_node_url() {
	local id=$1
	echo "http://localhost:$((8080 + id))"
}

raft_valid_node_id() {
	[[ "$1" =~ ^[1-9][0-9]*$ ]]
}

raft_first_node_id() {
	local ids=($RAFT_NODE_IDS)
	echo "${ids[0]}"
}

# --- Leader-hint plumbing (X-Raft-Leader-Id header) ---

_raft_hdr_file=""

raft_init() {
	_raft_hdr_file=$(mktemp)
	trap 'rm -f "$_raft_hdr_file"' EXIT
}

raft_leader_hint() {
	[[ -f "$_raft_hdr_file" ]] || return 0
	sed -n 's/^X-Raft-Leader-Id: *\([0-9][0-9]*\).*/\1/p' "$_raft_hdr_file" | head -1
}

raft_parse_body() {
	echo "$1" | sed -e 's/HTTP_STATUS\:.*//g'
}

raft_parse_status() {
	echo "$1" | tr -d '\n' | sed -e 's/.*HTTP_STATUS://'
}

# Try request, follow leader hint once on non-200.
# Usage: raft_request <do_fn> <initial_node_id>
#   do_fn receives a single arg: base URL.  Must use $_raft_hdr_file for --dump-header.
raft_request() {
	local do_fn=$1 node_id=$2

	local base response body status hint hint_base
	base=$(raft_node_url "$node_id")
	response=$("$do_fn" "$base")
	status=$(raft_parse_status "$response")

	if [[ "$status" != "200" ]]; then
		hint=$(raft_leader_hint)
		if [[ -n "$hint" && "$hint" != "$node_id" ]]; then
			echo "↳ Node ${node_id} is not leader; redirecting to node ${hint}..."
			hint_base=$(raft_node_url "$hint")
			response=$("$do_fn" "$hint_base")
			status=$(raft_parse_status "$response")
		fi
	fi

	body=$(raft_parse_body "$response")
	if [[ "$status" == "200" ]]; then
		echo "✅ ${body}"
		exit 0
	fi
	echo "❌ HTTP ${status}: ${body}" >&2
	exit 1
}

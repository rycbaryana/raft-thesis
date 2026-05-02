#!/usr/bin/env bash
# Kill then start one node (chaos / recovery checks).
# Usage: ./restart-node.sh <1|2|3> [log-level] [storage]

set -euo pipefail
DIR="$(cd "$(dirname "$0")" && pwd)"
ID="${1:-}"
LOG_LEVEL="${2:-info}"
STORAGE="${3:-memory}"

if [[ ! "$ID" =~ ^[123]$ ]]; then
	echo "Usage: $0 <1|2|3> [debug|info|warn|error] [memory|disk]" >&2
	exit 1
fi

"$DIR/kill-node.sh" "$ID" || true
sleep 0.3
"$DIR/start-node.sh" "$ID" "$LOG_LEVEL" "$STORAGE"

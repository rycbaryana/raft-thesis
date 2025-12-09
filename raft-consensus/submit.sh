#!/bin/bash

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

if [ -z "$1" ]; then
  echo -e "${YELLOW}Usage:${NC} ./submit.sh <node_id> [message]"
  echo "Example: ./submit.sh 1 'Hello World'"
  exit 1
fi

NODE_ID=$1
MSG=${2:-"SET X=10"}
PORT=$((8080 + NODE_ID))
BASE_URL="http://localhost:$PORT/submit"

echo -n "Sending command to Node $NODE_ID... "

RESPONSE=$(curl -G -s -w "\n%{http_code}" --data-urlencode "cmd=$MSG" "$BASE_URL")
CURL_EXIT=$?

if [ $CURL_EXIT -ne 0 ]; then
    echo -e "\n${RED}[ERROR] Connection Refused.${NC}"
    echo "Node $NODE_ID seems to be dead or not started."
    exit 1
fi

HTTP_CODE=$(echo "$RESPONSE" | tail -n1)
BODY=$(echo "$RESPONSE" | sed '$d')

if [ "$HTTP_CODE" -eq 200 ]; then
    echo -e "${GREEN}[SUCCESS]${NC}"
    echo "Check logs: $BODY"
elif [ "$HTTP_CODE" -eq 503 ] || [ "$HTTP_CODE" -eq 400 ]; then
    echo -e "${YELLOW}[REJECTED]${NC}"
    echo "Server Message: $BODY"
else
    echo -e "${RED}[ERROR $HTTP_CODE]${NC}"
    echo "Response: $BODY"
fi

#!/usr/bin/env bash
#
# seed.sh — populates the running carrier-match-service with sample data.
#
# Data lives only in memory (see store.go's MemStore), so it disappears
# every time the server restarts. Run this script again after each restart
# to repopulate it.
#
# Usage:
#   ./seed.sh
#
# Requires the server already running (`go run .` in another terminal)
# and `jq` installed for pretty-printing (brew install jq if you don't
# have it — the script still works without it, just less readable output).

set -euo pipefail

BASE_URL="http://localhost:8080"

have_jq() { command -v jq >/dev/null 2>&1; }

pretty() {
  if have_jq; then jq .; else cat; fi
}

echo "== Checking server is up =="
if ! curl -sf "${BASE_URL}/health" > /dev/null; then
  echo "ERROR: server not reachable at ${BASE_URL}. Start it first with: go run ."
  exit 1
fi
echo "Server is up."
echo

echo "== Creating carriers =="

declare -a CARRIERS=(
  '{"name":"Acme Freight","address":"Chicago, IL","capacity_lbs":40000}'
  '{"name":"Rocky Mountain Haulers","address":"Denver, CO","capacity_lbs":26000}'
  '{"name":"Pacific Coast Logistics","address":"Los Angeles, CA","capacity_lbs":45000}'
  '{"name":"Lone Star Transport","address":"Dallas, TX","capacity_lbs":15000}'
  '{"name":"Empire State Carriers","address":"New York, NY","capacity_lbs":32000}'
)

for carrier_json in "${CARRIERS[@]}"; do
  name=$(echo "$carrier_json" | grep -o '"name":"[^"]*"' | cut -d'"' -f4)
  echo "  Registering: $name"
  response=$(curl -sf -X POST "${BASE_URL}/carriers" -d "$carrier_json")
  echo "$response" | pretty
  echo
  # Nominatim's usage policy asks for ~1 request/second — this loop is the
  # real reason to respect that, not just politeness.
  sleep 1
done

echo "== Creating a sample shipment =="
SHIPMENT_JSON='{"origin_address":"Denver, CO","weight_lbs":12000}'
shipment_response=$(curl -sf -X POST "${BASE_URL}/shipments" -d "$SHIPMENT_JSON")
echo "$shipment_response" | pretty
echo

SHIPMENT_ID=$(echo "$shipment_response" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)

if [ -z "$SHIPMENT_ID" ]; then
  echo "Could not parse shipment ID from response — check the output above."
  exit 1
fi

echo "== Fetching ranked matches for shipment $SHIPMENT_ID =="
sleep 1
curl -sf "${BASE_URL}/shipments/${SHIPMENT_ID}/matches" | pretty

echo
echo "Done. Shipment ID for further testing: ${SHIPMENT_ID}"
echo "(Try: curl ${BASE_URL}/shipments/${SHIPMENT_ID}/matches )"

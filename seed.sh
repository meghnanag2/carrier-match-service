#!/usr/bin/env bash
#
# seed.sh — populates the running carrier-match-service with sample data
# and walks through the full lifecycle: register shipper -> register
# carriers -> register shipment -> get ranked matches -> dispatch ->
# advance status. Demonstrates every endpoint in one run.
#
# Data lives in a real SQLite file now (carrier_match.db), so unlike the
# earlier in-memory version of this project, it DOES survive a restart —
# re-running this script will add a second shipper/set of carriers rather
# than "restoring" anything.
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
pretty() { if have_jq; then jq .; else cat; fi }
extract_id() { grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4; }

echo "== Checking server is up =="
if ! curl -sf "${BASE_URL}/health" > /dev/null; then
  echo "ERROR: server not reachable at ${BASE_URL}. Start it first with: go run ."
  exit 1
fi
echo "Server is up."
echo

echo "== Registering a shipper =="
SHIPPER_JSON='{"name":"Acme Furniture Co.","email":"ops@acmefurniture.example"}'
shipper_response=$(curl -sf -X POST "${BASE_URL}/shippers" -d "$SHIPPER_JSON")
echo "$shipper_response" | pretty
SHIPPER_ID=$(echo "$shipper_response" | extract_id)
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
  curl -sf -X POST "${BASE_URL}/carriers" -d "$carrier_json" | pretty
  echo
  sleep 1
done

echo "== Creating a shipment for shipper $SHIPPER_ID =="
SHIPMENT_JSON="{\"shipper_id\":\"${SHIPPER_ID}\",\"origin_address\":\"Denver, CO\",\"weight_lbs\":12000}"
shipment_response=$(curl -sf -X POST "${BASE_URL}/shipments" -d "$SHIPMENT_JSON")
echo "$shipment_response" | pretty
SHIPMENT_ID=$(echo "$shipment_response" | extract_id)
echo

echo "== Fetching ranked matches (includes estimated price) =="
sleep 1
matches_response=$(curl -sf "${BASE_URL}/shipments/${SHIPMENT_ID}/matches")
echo "$matches_response" | pretty
echo

echo "== Dispatching to the top-ranked feasible carrier =="
if have_jq; then
  TOP_CARRIER_ID=$(echo "$matches_response" | jq -r '[.[] | select(.feasible == true)][0].carrier_id')
else
  echo "jq not installed — skipping automatic dispatch. Manually run:"
  echo "  curl -X POST ${BASE_URL}/shipments/${SHIPMENT_ID}/dispatch -d '{\"carrier_id\":\"<id from matches above>\"}'"
  TOP_CARRIER_ID=""
fi

if [ -n "$TOP_CARRIER_ID" ] && [ "$TOP_CARRIER_ID" != "null" ]; then
  dispatch_response=$(curl -sf -X POST "${BASE_URL}/shipments/${SHIPMENT_ID}/dispatch" \
    -d "{\"carrier_id\":\"${TOP_CARRIER_ID}\"}")
  echo "$dispatch_response" | pretty
  if have_jq; then
    payment_status=$(echo "$dispatch_response" | jq -r '.payment_status // "not configured (set STRIPE_SECRET_KEY to enable)"')
    echo "  Payment status: $payment_status"
  fi
  echo

  echo "== Advancing status: dispatched -> in_transit =="
  curl -sf -X PATCH "${BASE_URL}/shipments/${SHIPMENT_ID}/status" -d '{"status":"in_transit"}' | pretty
  echo

  echo "== Advancing status: in_transit -> delivered (captures the Stripe payment hold, if configured) =="
  curl -sf -X PATCH "${BASE_URL}/shipments/${SHIPMENT_ID}/status" -d '{"status":"delivered"}' | pretty
  echo

  echo "== Trying an illegal transition (delivered -> pending) — should be rejected with 409 =="
  curl -s -X PATCH "${BASE_URL}/shipments/${SHIPMENT_ID}/status" -d '{"status":"pending"}' | pretty
  echo
fi

echo
echo "Done."
echo "  Shipper ID:  ${SHIPPER_ID}"
echo "  Shipment ID: ${SHIPMENT_ID}"

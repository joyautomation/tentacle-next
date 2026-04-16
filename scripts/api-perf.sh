#!/usr/bin/env bash
# API performance test battery for tentacle-dist container.
# Runs curl against each endpoint, reports timing, and flags anything slow.
#
# Usage:
#   bash scripts/api-perf.sh              # default threshold: 500ms
#   bash scripts/api-perf.sh 200          # custom threshold in ms

set -euo pipefail

THRESHOLD_MS="${1:-500}"
CONTAINER="tentacle-dist"
BASE="http://localhost:4000/api/v1"

# Verify container is running
if ! incus info "$CONTAINER" &>/dev/null; then
  echo "ERROR: container '$CONTAINER' not found. Run /deploy-dist first."
  exit 1
fi

# Endpoint list: "METHOD PATH DESCRIPTION"
ENDPOINTS=(
  "GET /services List services"
  "GET /variables List all variables"
  "GET /variables?moduleId=ethernetip List EIP variables"
  "GET /gateways List gateways"
  "GET /gateways/gateway Get gateway config"
  "GET /gateways/browse-states Browse states"
  "GET /gateways/gateway/browse-cache/rtu45 Browse cache (device)"
  "GET /orchestrator/modules List modules"
  "GET /orchestrator/desired-services Desired services"
  "GET /orchestrator/service-statuses Service statuses"
  "GET /config All config"
  "GET /mode Deployment mode"
  "GET /system/hostname Hostname"
  "GET /system/version Version"
  "GET /system/releases List releases"
  "GET /system/service Service status"
  "GET /services/gateway/logs Gateway logs"
  "GET /services/ethernetip/logs EIP logs"
  "GET /services/mqtt/logs MQTT logs"
  "GET /mqtt/metrics MQTT metrics"
  "GET /telemetry/status Telemetry status"
  "GET /network/interfaces Network interfaces"
  "GET /nats/traffic NATS traffic"
  "GET /export Export manifest"
)

PASS=0
FAIL=0
ERRORS=0
RESULTS=()

printf "\n%-50s %10s %8s\n" "ENDPOINT" "TIME" "STATUS"
printf "%-50s %10s %8s\n" "$(printf '%0.s─' {1..50})" "$(printf '%0.s─' {1..10})" "$(printf '%0.s─' {1..8})"

for entry in "${ENDPOINTS[@]}"; do
  METHOD=$(echo "$entry" | awk '{print $1}')
  EP=$(echo "$entry" | awk '{print $2}')
  DESC=$(echo "$entry" | awk '{for(i=3;i<=NF;i++) printf "%s%s",$i,(i<NF?" ":""); print ""}')

  RESULT=$(incus exec "$CONTAINER" -- curl -s -o /dev/null \
    -w '%{http_code} %{time_total}' \
    -X "$METHOD" \
    "${BASE}${EP}" 2>&1) || true

  HTTP_CODE=$(echo "$RESULT" | awk '{print $1}')
  TIME_SEC=$(echo "$RESULT" | awk '{print $2}')
  TIME_MS=$(echo "$TIME_SEC" | awk '{printf "%.0f", $1 * 1000}')

  if [[ "$HTTP_CODE" -ge 400 ]]; then
    STATUS="ERR:${HTTP_CODE}"
    ((ERRORS++)) || true
  elif [[ "$TIME_MS" -gt "$THRESHOLD_MS" ]]; then
    STATUS="SLOW"
    ((FAIL++)) || true
  else
    STATUS="OK"
    ((PASS++)) || true
  fi

  printf "%-50s %8sms %8s\n" "$DESC ($EP)" "$TIME_MS" "$STATUS"
  RESULTS+=("$STATUS $TIME_MS $DESC")
done

printf "\n"
printf "Threshold: %sms\n" "$THRESHOLD_MS"
printf "Results:   %d passed, %d slow, %d errors (out of %d)\n" \
  "$PASS" "$FAIL" "$ERRORS" "${#ENDPOINTS[@]}"

if [[ "$FAIL" -gt 0 || "$ERRORS" -gt 0 ]]; then
  exit 1
fi

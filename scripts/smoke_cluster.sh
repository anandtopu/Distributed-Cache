#!/usr/bin/env sh
set -eu

wait_ok() {
  url="$1"
  deadline=$(( $(date +%s) + 30 ))
  while [ "$(date +%s)" -lt "$deadline" ]; do
    if curl -fsS "$url" >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.3
  done
  echo "Timed out waiting for $url" >&2
  exit 1
}

echo "Starting cluster via docker compose..."
docker compose up -d >/dev/null

wait_ok "http://localhost:8080/healthz"
wait_ok "http://localhost:8081/healthz"
wait_ok "http://localhost:8082/healthz"

key="foo"
value_b64="$(printf "bar" | base64)"

curl -fsS -X POST "http://localhost:8081/$key" -H 'content-type: application/json' \
  -d "{\"value\":\"$value_b64\",\"ttl_ms\":60000}" >/dev/null

curl -fsS "http://localhost:8080/$key"; echo
curl -fsS "http://localhost:8082/$key"; echo

echo "Smoke test OK"

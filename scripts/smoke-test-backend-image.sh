#!/usr/bin/env bash
set -Eeuo pipefail

repository_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
run_suffix="${GITHUB_RUN_ID:-local}-${GITHUB_RUN_ATTEMPT:-0}-$$-${RANDOM}"
image_name="kickertool-standings-backend-smoke:${run_suffix}"
container_name="kickertool-standings-backend-smoke-${run_suffix}"
response_file="$(mktemp)"

cleanup() {
  exit_code=$?
  trap - EXIT INT TERM

  if (( exit_code != 0 )); then
    docker logs "${container_name}" >&2 2>/dev/null || true
  fi
  docker rm --force --volumes "${container_name}" >/dev/null 2>&1 || true
  docker image rm --force "${image_name}" >/dev/null 2>&1 || true
  rm -f "${response_file}"

  exit "${exit_code}"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

docker build --tag "${image_name}" "${repository_root}/backend"
MSYS_NO_PATHCONV=1 docker run --detach \
  --name "${container_name}" \
  --publish 127.0.0.1::8080 \
  --tmpfs /data:rw,noexec,nosuid,size=64m,mode=1777 \
  --env DB_PATH=/data/tournaments.db \
  --env TOURNAMENT_SOURCE=html \
  --env TOURNAMENT_HTML_URL=http://127.0.0.1:1 \
  --env HTTP_TIMEOUT=1s \
  --env MAX_RETRIES=0 \
  --env CRAWL_INTERVAL=24h \
  "${image_name}" >/dev/null

published_address="$(docker port "${container_name}" 8080/tcp | head -n 1)"
if [[ -z "${published_address}" ]]; then
  echo "backend container did not publish port 8080" >&2
  exit 1
fi
base_url="http://${published_address}"

ready=false
for _ in $(seq 1 30); do
  if curl --fail --silent "${base_url}/healthz" >/dev/null 2>&1; then
    ready=true
    break
  fi
  if [[ "$(docker inspect --format '{{.State.Running}}' "${container_name}" 2>/dev/null || true)" != "true" ]]; then
    break
  fi
  sleep 1
done
if [[ "${ready}" != "true" ]]; then
  echo "backend container did not become ready" >&2
  exit 1
fi

status_code="$(curl --silent --show-error --output "${response_file}" --write-out '%{http_code}' "${base_url}/api/v1/public/rankings")"
if [[ "${status_code}" != "200" ]]; then
  echo "expected rankings endpoint to return HTTP 200, got ${status_code}" >&2
  cat "${response_file}" >&2
  exit 1
fi

python3 - "${response_file}" <<'PY'
import json
import pathlib
import sys

payload = json.loads(pathlib.Path(sys.argv[1]).read_text(encoding="utf-8"))
for field in ("items", "availableYears"):
    if payload.get(field) != []:
        raise SystemExit(f"expected {field} to be an empty JSON array, got {payload.get(field)!r}")
PY

echo "backend image smoke test passed"

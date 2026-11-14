#!/usr/bin/env bash
# List, read, events, cancel, and rerun one packaging run.
# Docker, PostgreSQL, and Temporal each have to be usable. A missing one
# fails this script. It is a local check and is not a hosted workflow.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${root}"

if ! command -v docker >/dev/null 2>&1; then
  echo "Docker is required for the packaging endpoint check and was not found" >&2
  exit 1
fi
if ! docker info >/dev/null 2>&1; then
  echo "Docker is required for the packaging endpoint check and is not running" >&2
  exit 1
fi

image="postgres@sha256:a1d02e4bd40c94d3bf2bdd3678c137388e76d9efcd23c285e9429d336a834b44"
name="conduit-packaging-e2e-$$"
cleanup() {
  if [[ -n "${temporal_pid:-}" ]]; then
    kill "${temporal_pid}" >/dev/null 2>&1 || true
    wait "${temporal_pid}" >/dev/null 2>&1 || true
  fi
  docker rm -f "${name}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

docker run --rm --detach --name "${name}" \
  --env POSTGRES_PASSWORD=conduit_test \
  --env POSTGRES_USER=conduit \
  --env POSTGRES_DB=conduit_test \
  --publish 127.0.0.1::5432 \
  "${image}" >/dev/null

ready=0
for _ in $(seq 1 90); do
  if docker exec "${name}" pg_isready --username conduit --dbname conduit_test >/dev/null 2>&1; then
    ready=$((ready + 1))
    [[ "${ready}" -ge 3 ]] && break
  else
    ready=0
  fi
  sleep 1
done
if [[ "${ready}" -lt 3 ]]; then
  echo "PostgreSQL is required for the packaging endpoint check and did not become ready" >&2
  docker logs "${name}" 2>&1 | tail -20 >&2
  exit 1
fi
port="$(docker port "${name}" 5432/tcp | awk -F: '/127[.]0[.]0[.]1/ {print $NF; exit}')"
if [[ ! "${port}" =~ ^[0-9]+$ ]]; then
  echo "PostgreSQL is required for the packaging endpoint check and its port could not be read" >&2
  exit 1
fi

if ! "${root}/scripts/fetch-temporal-cli.sh" >/dev/null; then
  echo "Temporal is required for the packaging endpoint check and its CLI could not be fetched" >&2
  exit 1
fi
cli="${root}/.cache/temporal/temporal"
if [[ ! -x "${cli}" ]]; then
  echo "Temporal is required for the packaging endpoint check and ${cli} is not executable" >&2
  exit 1
fi

temporal_db="$(mktemp -d)/temporal.db"
temporal_port="${PACKAGING_E2E_TEMPORAL_PORT:-7234}"
"${cli}" server start-dev \
  --ip 127.0.0.1 \
  --ui-ip 127.0.0.1 \
  --ui-disable-news-fetch \
  --db-filename "${temporal_db}" \
  --port "${temporal_port}" \
  --ui-port $((temporal_port + 1000)) \
  --log-level warn >/tmp/conduit-packaging-e2e-temporal.log 2>&1 &
temporal_pid=$!

temporal_ready=0
for _ in $(seq 1 60); do
  if (echo >/dev/tcp/127.0.0.1/"${temporal_port}") >/dev/null 2>&1; then
    temporal_ready=1
    break
  fi
  if ! kill -0 "${temporal_pid}" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
if [[ "${temporal_ready}" -ne 1 ]]; then
  echo "Temporal is required for the packaging endpoint check and did not accept connections on ${temporal_port}" >&2
  tail -20 /tmp/conduit-packaging-e2e-temporal.log >&2 || true
  exit 1
fi

export PACKAGING_E2E=1
export PACKAGING_TEMPORAL_ADDRESS="127.0.0.1:${temporal_port}"
export POSTGRES_DSN="postgres://conduit:conduit_test@127.0.0.1:${port}/conduit_test?sslmode=disable"
go test -count=1 -timeout 4m ./internal/packagingworkflow/runtime -run TestPackagingEndpoints

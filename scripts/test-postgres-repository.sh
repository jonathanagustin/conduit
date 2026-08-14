#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
image="postgres@sha256:a1d02e4bd40c94d3bf2bdd3678c137388e76d9efcd23c285e9429d336a834b44"
name="conduit-postgres-repository-${$}"

command -v docker >/dev/null || { echo "docker is required for the PostgreSQL repository gate" >&2; exit 1; }
docker info >/dev/null
cleanup() {
  docker rm -f "${name}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

docker run --rm --detach --name "${name}" \
  --env POSTGRES_PASSWORD=conduit_test \
  --env POSTGRES_USER=conduit \
  --env POSTGRES_DB=conduit_test \
  --publish 127.0.0.1::5432 \
  "${image}" >/dev/null

# A single pg_isready is not enough to call this server up. The postgres image
# runs initdb against a temporary server on a socket, shuts it down, then starts
# the real one; a probe landing in that window answers ready for a server that
# is about to go away. The loop would break there and the verification a moment
# later would fail under `set -e`, which reads as a broken gate rather than a
# race. Requiring consecutive successes spans the restart: the streak resets
# when the temporary server stops, so only the real one can satisfy it.
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
[[ "${ready}" -ge 3 ]] || {
  echo "PostgreSQL never became consistently ready" >&2
  docker logs "${name}" 2>&1 | tail -20 >&2
  exit 1
}

port="$(docker port "${name}" 5432/tcp | awk -F: '/127[.]0[.]0[.]1/ {print $NF; exit}')"
[[ "${port}" =~ ^[0-9]+$ ]] || { echo "could not resolve the disposable PostgreSQL port" >&2; exit 1; }

cd "${root}"
dsn="postgres://conduit:conduit_test@127.0.0.1:${port}/conduit_test?sslmode=disable"
CONDUIT_TEST_POSTGRES_DSN="${dsn}" go test -race ./internal/controlplane -run '^TestPostgres'
CONDUIT_TEST_POSTGRES_DSN="${dsn}" go test -race ./internal/delivery/reconciliation/adapters/postgres -run '^TestPostgres'
CONDUIT_TEST_POSTGRES_DSN="${dsn}" go test -race ./cmd/platform-delivery -run '^TestPostgres'

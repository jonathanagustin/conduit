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

for _ in $(seq 1 60); do
  docker exec "${name}" pg_isready --username conduit --dbname conduit_test >/dev/null 2>&1 && break
  sleep 1
done
docker exec "${name}" pg_isready --username conduit --dbname conduit_test >/dev/null

port="$(docker port "${name}" 5432/tcp | awk -F: '/127[.]0[.]0[.]1/ {print $NF; exit}')"
[[ "${port}" =~ ^[0-9]+$ ]] || { echo "could not resolve the disposable PostgreSQL port" >&2; exit 1; }

cd "${root}"
CONDUIT_TEST_POSTGRES_DSN="postgres://conduit:conduit_test@127.0.0.1:${port}/conduit_test?sslmode=disable" \
  go test -race ./internal/controlplane ./internal/delivery/reconciliation/adapters/postgres -run '^TestPostgres'

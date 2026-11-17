#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# PostgreSQL 18 on alpine 3.24, built 2026-08-13 (resolved from the registry:
# org.opencontainers.image.source is docker-library/postgres#…:18/alpine3.24).
#
# The version is written down because nothing else records it. The pin carries a
# digest and no tag, so `18` is not recoverable from this file, and Renovate does
# not manage it either -- its dockerfile manager only matches Dockerfile and
# Containerfile paths, and this is a shell script. Whoever changes the digest has
# to update this line, and the restore drill will fail loudly if the client and
# server majors stop matching.
image="postgres@sha256:a1d02e4bd40c94d3bf2bdd3678c137388e76d9efcd23c285e9429d336a834b44"
name="conduit-postgres-repository-${$}"

command -v docker >/dev/null || { echo "docker is required for the PostgreSQL repository gate" >&2; exit 1; }
docker info >/dev/null

cleanup() {
  docker rm -f -v "${name}" >/dev/null 2>&1 || true
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

# The restore drill takes a real pg_dump, and it uses the pinned image's own
# client rather than the runner's.
#
# That is not a convenience. pg_dump refuses a server newer than itself, and
# these two versions are chosen by different people: the server is the digest
# above, which today is PostgreSQL 18, while the client is whatever the CI
# runner image ships, which is 16. They already diverge, so a drill built on the
# runner's client would fail on its first dispatch -- and the alternatives are
# worse. Installing a matching client adds an unpinned apt source to a
# repository that pins everything else by digest, and downgrading the server to
# match the runner would test backups against a PostgreSQL nobody runs.
#
# The container's client matches its server by construction and is already
# pinned. The archive moves over the drill's stdin and stdout because the client
# does not share this filesystem, and the DSN below is the server's address from
# inside the container rather than from here.
export CONDUIT_TEST_PG_TOOL_PREFIX="docker exec -i ${name}"
export CONDUIT_TEST_PG_TOOL_DSN="postgres://conduit:conduit_test@127.0.0.1:5432/conduit_test?sslmode=disable"
# No -run filters, deliberately. The '^TestPostgres' filter these two lines
# carried encodes a naming convention nobody wrote down, and it silently
# excluded tests added with the migration ledger: a database-backed test whose
# name does not begin with TestPostgres skips without a DSN locally AND is
# filtered out here, so it runs nowhere at all. The database-free tests in these
# packages are seconds at most, so running them again costs little and removes
# the trap. cmd/platform-delivery keeps its filter: that package's other tests
# start listeners and are covered by the main Test gate.
CONDUIT_TEST_POSTGRES_DSN="${dsn}" go test -race ./internal/governance/adapters/auditpostgres
CONDUIT_TEST_POSTGRES_DSN="${dsn}" go test -race ./internal/controlplane
CONDUIT_TEST_POSTGRES_DSN="${dsn}" go test -race ./internal/delivery/reconciliation/adapters/postgres
CONDUIT_TEST_POSTGRES_DSN="${dsn}" go test -race ./internal/notifications/adapters/postgres
CONDUIT_TEST_POSTGRES_DSN="${dsn}" go test -race ./internal/spaces/adapters/postgres
CONDUIT_TEST_POSTGRES_DSN="${dsn}" go test -race ./internal/productassets/adapters/postgres
CONDUIT_TEST_POSTGRES_DSN="${dsn}" go test -race ./internal/changeactivity/adapters/postgres
CONDUIT_TEST_POSTGRES_DSN="${dsn}" go test -race ./internal/debugsessions/adapters/postgres
# The assistant conversation store runs the same conformance suite its
# in-memory adapter runs on every `go test`, plus the schema's own bounds.
CONDUIT_TEST_POSTGRES_DSN="${dsn}" go test -race ./internal/assistant/adapters/threadpostgres
# This package opens two independent pools and writes concurrently; the
# cmd/platform-delivery drill below separately proves its migration and one
# exact resolution survive the pinned-client dump/restore path.
CONDUIT_TEST_POSTGRES_DSN="${dsn}" go test -race ./internal/gitless/adapters/resolutionlogpostgres
# internal/pgmigrate is here because its dry-run tests need a database. Without
# this line they would skip locally for want of a DSN and be reached by nothing
# in CI -- the same "runs nowhere" trap the -run filters above used to set.
#
# This package is also the slowest line here, at about thirteen seconds, and the
# reason is worth knowing before anyone trims it: its crash trials re-execute
# this test binary as a child process and SIGKILL it sixty times, to check that
# a migration interrupted between its DDL and its ledger row leaves the two
# agreeing. Each of those is a process start against this server. Turn the count
# up with CONDUIT_PGMIGRATE_CRASH_TRIALS when investigating; the default is what
# runs here.
CONDUIT_TEST_POSTGRES_DSN="${dsn}" go test -race ./internal/pgmigrate
CONDUIT_TEST_POSTGRES_DSN="${dsn}" go test -race ./cmd/platform-delivery -run '^TestPostgres'


#!/usr/bin/env bash
# Real synchronous PostgreSQL promotion with active Hub/Agent protocol traffic.
# The two containers, network and volume belong only to this invocation.
set -euo pipefail
umask 077
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${root}"
[[ -z "$(git status --porcelain --untracked-files=normal)" ]] || { echo 'failover evidence requires a committed clean checkout' >&2; exit 1; }
commit="$(git rev-parse HEAD)"
tree="$(git rev-parse 'HEAD^{tree}')"
engine="${CONDUIT_CONTAINER_ENGINE:-docker}"
[[ "${engine}" != *[[:space:]]* ]] || { echo 'CONDUIT_CONTAINER_ENGINE must name one executable' >&2; exit 1; }
"${engine}" info >/dev/null
image='postgres@sha256:a1d02e4bd40c94d3bf2bdd3678c137388e76d9efcd23c285e9429d336a834b44'
state="$(mktemp -d "${TMPDIR:-/tmp}/conduit-fleet-failover.XXXXXXXX")"
name="${state##*/}"
primary="${name}-primary"
standby="${name}-standby"
volume="${name}-data"
load_pid=''
cleanup() {
  result=$?
  if [[ -n "${load_pid}" ]]; then kill "${load_pid}" >/dev/null 2>&1 || true; wait "${load_pid}" >/dev/null 2>&1 || true; fi
  if [[ "${result}" -ne 0 ]]; then
    "${engine}" logs "${primary}" >"${state}/primary.log" 2>&1 || true
    "${engine}" logs "${standby}" >"${state}/standby.log" 2>&1 || true
    printf 'Failed PostgreSQL fixture evidence: %s\n' "${state}" >&2
  fi
  "${engine}" rm --force "${primary}" "${standby}" >/dev/null 2>&1 || true
  "${engine}" volume rm "${volume}" >/dev/null 2>&1 || true
  "${engine}" network rm "${name}" >/dev/null 2>&1 || true
}
trap cleanup EXIT
"${engine}" network create "${name}" >/dev/null
"${engine}" run --detach --name "${primary}" --network "${name}" --network-alias primary \
  --publish 127.0.0.1::5432 --memory 192m --cpus 1 \
  --env POSTGRES_USER=conduit --env POSTGRES_PASSWORD=conduit_test --env POSTGRES_DB=conduit_test \
  "${image}" -c wal_level=replica -c max_wal_senders=4 -c max_replication_slots=4 -c shared_buffers=16MB >/dev/null
ready() {
  local container="$1" count=0
  for _ in $(seq 1 90); do
    if "${engine}" exec "${container}" pg_isready --username conduit --dbname conduit_test >/dev/null 2>&1; then count=$((count+1)); [[ "${count}" -ge 3 ]] && return 0; else count=0; fi
    sleep 1
  done
  echo 'PostgreSQL fixture did not become consistently ready' >&2
  return 1
}
ready "${primary}"
"${engine}" exec --user postgres "${primary}" sh -c 'printf "%s\n" "host replication conduit all scram-sha-256" >> "$PGDATA/pg_hba.conf"'
"${engine}" exec "${primary}" psql --username conduit --dbname conduit_test -c 'SELECT pg_reload_conf()' >/dev/null
"${engine}" volume create "${volume}" >/dev/null
"${engine}" run --rm --volume "${volume}:/var/lib/postgresql" --entrypoint sh "${image}" -c 'chown postgres:postgres /var/lib/postgresql'
"${engine}" run --detach --name "${standby}" --network "${name}" --publish 127.0.0.1::5432 \
  --memory 192m --cpus 1 --user postgres --volume "${volume}:/var/lib/postgresql" \
  --env PGPASSWORD=conduit_test --entrypoint sh "${image}" -c \
  'pg_basebackup --dbname="host=primary user=conduit application_name=capacity_standby" --pgdata="$PGDATA" --write-recovery-conf --wal-method=stream --checkpoint=fast --no-password && exec postgres -c shared_buffers=16MB' >/dev/null
ready "${standby}"
"${engine}" exec "${primary}" psql --username conduit --dbname conduit_test -c "ALTER SYSTEM SET synchronous_standby_names='capacity_standby'" >/dev/null
"${engine}" exec "${primary}" psql --username conduit --dbname conduit_test -c "ALTER SYSTEM SET synchronous_commit='remote_apply'" >/dev/null
"${engine}" exec "${primary}" psql --username conduit --dbname conduit_test -c 'SELECT pg_reload_conf()' >/dev/null
synced=false
for _ in $(seq 1 30); do
  if [[ "$("${engine}" exec "${primary}" psql --username conduit --dbname conduit_test --tuples-only --no-align -c "SELECT count(*) FROM pg_stat_replication WHERE application_name='capacity_standby' AND sync_state='sync'")" == 1 ]]; then synced=true; break; fi
  sleep 1
done
[[ "${synced}" == true ]] || { echo 'synchronous standby never joined' >&2; exit 1; }
primary_port="$("${engine}" port "${primary}" 5432/tcp | awk -F: '/127[.]0[.]0[.]1/ {print $NF; exit}')"
standby_port="$("${engine}" port "${standby}" 5432/tcp | awk -F: '/127[.]0[.]0[.]1/ {print $NF; exit}')"
[[ "${primary_port}" =~ ^[0-9]+$ && "${standby_port}" =~ ^[0-9]+$ ]] || { echo 'cannot resolve fixture ports' >&2; exit 1; }
CONDUIT_CAPACITY_FAILOVER_RUN=1 CONDUIT_CAPACITY_COMMIT="${commit}" \
  CONDUIT_CAPACITY_PRIMARY_DSN="postgres://conduit:conduit_test@127.0.0.1:${primary_port}/conduit_test?sslmode=disable&connect_timeout=5" \
  CONDUIT_CAPACITY_STANDBY_DSN="postgres://conduit:conduit_test@127.0.0.1:${standby_port}/conduit_test?sslmode=disable&connect_timeout=5" \
  CONDUIT_CAPACITY_READY_FILE="${state}/ready" CONDUIT_CAPACITY_TARGET_FILE="${state}/target" \
  CONDUIT_CAPACITY_REPORT="${state}/report.json" \
  go test ./internal/hub/composition -run '^TestFleetPostgresPromotionReconciliation$' -count=1 -v -timeout 2m >"${state}/go-test.log" 2>&1 &
load_pid=$!
for _ in $(seq 1 90); do
  [[ -s "${state}/ready" ]] && break
  if ! kill -0 "${load_pid}" 2>/dev/null; then cat "${state}/go-test.log"; exit 1; fi
  sleep 1
done
[[ -s "${state}/ready" ]] || { echo 'real reconciliation did not start before fault' >&2; exit 1; }
"${engine}" stop --time 0 "${primary}" >/dev/null
"${engine}" exec --user postgres "${standby}" sh -c 'pg_ctl --pgdata="$PGDATA" promote --wait --timeout=30' >/dev/null
printf '127.0.0.1:%s\n' "${standby_port}" >"${state}/target.new"
mv "${state}/target.new" "${state}/target"
if wait "${load_pid}"; then load_pid=''; else load_pid=''; cat "${state}/go-test.log"; exit 1; fi
cat "${state}/go-test.log"
[[ "$(git rev-parse HEAD)" == "${commit}" && -z "$(git status --porcelain --untracked-files=normal)" ]] || { echo 'checkout changed during failover test' >&2; exit 1; }
hash_file() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then shasum -a 256 "$1" | awk '{print $1}'
  else echo 'sha256sum or shasum is required for failover evidence' >&2; return 1
  fi
}
report_sha="$(hash_file "${state}/report.json")"
log_sha="$(hash_file "${state}/go-test.log")"
printf '{"schema_version":1,"kind":"CONDUIT_POSTGRES_PROMOTION","result":"PASS","commit":"%s","tree":"%s","report_sha256":"%s","log_sha256":"%s","production_capacity_established":false}\n' \
  "${commit}" "${tree}" "${report_sha}" "${log_sha}" > "${state}/receipt.json"
printf 'Real PostgreSQL promotion evidence: %s\n' "${state}"

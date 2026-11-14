#!/usr/bin/env bash
# Stop the local Hub, packaging worker, and Temporal process started by
# demo-seed.sh, and delete their state directory.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
state_dir="${CONDUIT_SEED_DIR:-/tmp/conduit-seed}"
if [[ -f "${state_dir}/pids" ]]; then
  while read -r pid; do
    [[ "${pid}" =~ ^[0-9]+$ ]] || continue
    kill "${pid}" >/dev/null 2>&1 || true
  done <"${state_dir}/pids"
  # Give the listeners a moment to release their ports, then clear anything
  # still attached to them.
  sleep 1
  while read -r pid; do
    [[ "${pid}" =~ ^[0-9]+$ ]] || continue
    kill -9 "${pid}" >/dev/null 2>&1 || true
  done <"${state_dir}/pids"
fi
docker rm -f conduit-seed-registry >/dev/null 2>&1 || true
pkill -f "${state_dir}/packaging" >/dev/null 2>&1 || true
pkill -f 'platform-delivery' >/dev/null 2>&1 || true
# go run reparents the server after it exits, so the pid file can miss it.
for port in 8081 7233 8233 5000; do
  while read -r pid; do
    [[ "${pid}" =~ ^[0-9]+$ ]] || continue
    kill "${pid}" >/dev/null 2>&1 || true
  done < <(ss -ltnp "sport = :${port}" 2>/dev/null | sed -n 's/.*pid=\([0-9][0-9]*\).*/\1/p' | sort -u)
done
sleep 1
rm -rf "${state_dir}"
echo "cleared ${state_dir}"
"${root}/scripts/build-console.sh"

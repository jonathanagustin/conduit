#!/usr/bin/env bash
# Start the Docker daemon, which the sandbox has installed and not running.
#
# scripts/test-postgres-repository.sh drives a real PostgreSQL through
# testcontainers, so without a daemon that gate cannot run at all -- it fails
# with "dial unix /var/run/docker.sock: no such file or directory", which reads
# like a broken repository and is a missing daemon.
#
# Unlike the Kubernetes bootstrap beside this, nothing needs patching here: the
# daemon works as shipped once started.
set -euo pipefail

log() { printf 'docker: %s\n' "$*"; }

[ "$(id -u)" -eq 0 ] || { printf 'docker: must run as root\n' >&2; exit 1; }

if docker info >/dev/null 2>&1; then
  log "already running"
  exit 0
fi

log "starting the daemon (log: /tmp/dockerd.log)"
nohup dockerd >/tmp/dockerd.log 2>&1 &
disown

for _ in $(seq 1 40); do
  docker info >/dev/null 2>&1 && { log "ready"; exit 0; }
  sleep 2
done

printf 'docker: FATAL: the daemon did not become ready; see /tmp/dockerd.log\n' >&2
exit 1

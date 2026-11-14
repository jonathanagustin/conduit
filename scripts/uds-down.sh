#!/usr/bin/env bash
# Tear down the local UDS Core k3d cluster created by scripts/uds-up.sh.
#
# The user-defined Docker network is left in place when other containers are
# still attached to it.
set -euo pipefail

cluster_name="${UDS_K3D_CLUSTER:-uds}"
network_name="${UDS_K3D_NETWORK:-uds-k3d}"
dry_run=false
print_plan=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --print-plan) print_plan=true ;;
    --dry-run) dry_run=true ;;
    -h|--help)
      cat <<EOF
Usage: $(basename "$0") [--dry-run] [--print-plan]
EOF
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      exit 2
      ;;
  esac
  shift
done

if "${print_plan}" || "${dry_run}"; then
  printf 'cluster=%s\n' "${cluster_name}"
  printf 'network=%s\n' "${network_name}"
  printf 'k3d cluster delete %s\n' "${cluster_name}"
  "${print_plan}" && exit 0
  "${dry_run}" && exit 0
fi

if command -v k3d >/dev/null 2>&1; then
  k3d cluster delete "${cluster_name}" >/dev/null 2>&1 || true
elif command -v uds >/dev/null 2>&1; then
  uds zarf tools kubectl config delete-context "k3d-${cluster_name}" >/dev/null 2>&1 || true
  echo "k3d is not on PATH; delete the cluster with k3d cluster delete ${cluster_name}" >&2
  exit 1
else
  echo "k3d is required to delete the cluster" >&2
  exit 1
fi

if command -v docker >/dev/null 2>&1 && docker network inspect "${network_name}" >/dev/null 2>&1; then
  # Leave the network when something else is still using it.
  containers="$(docker network inspect -f '{{range .Containers}}{{.Name}} {{end}}' "${network_name}" | tr -d '[:space:]')"
  if [[ -z "${containers}" ]]; then
    docker network rm "${network_name}" >/dev/null
  fi
fi

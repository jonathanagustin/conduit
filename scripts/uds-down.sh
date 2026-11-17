#!/usr/bin/env bash
# Tear down the local UDS Core k3d cluster created by scripts/uds-up.sh.
#
# The user-defined Docker network is left in place when other containers are
# still attached to it.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${root}"
source "${root}/scripts/lib/local-uds.sh"
conduit_uds_configure "${root}"
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
  printf 'context=%s\n' "${kubecontext}"
  printf 'kubeconfig=%s\n' "${kubeconfig}"
  printf 'k3d cluster delete %s\n' "${cluster_name}"
  "${print_plan}" && exit 0
  "${dry_run}" && exit 0
fi

conduit_uds_require k3d
conduit_uds_engine
export KUBECONFIG="${kubeconfig}"
if ! k3d cluster delete "${cluster_name}"; then
  echo 'k3d cluster deletion failed; no further cleanup was attempted' >&2
  exit 1
fi

if "${engine}" network inspect "${network_name}" >/dev/null 2>&1; then
  # Leave the network when something else is still using it.
  containers="$("${engine}" network inspect -f '{{range .Containers}}{{.Name}} {{end}}' "${network_name}" | tr -d '[:space:]')"
  if [[ -z "${containers}" ]]; then
    "${engine}" network rm "${network_name}" >/dev/null
  fi
fi

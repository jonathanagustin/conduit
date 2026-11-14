#!/usr/bin/env bash
# Bring up UDS Core slim-dev on a local k3d cluster.
#
# This host's Docker daemon may run without a default bridge. k3d needs a
# user-defined network in that case, so this script creates one first and
# passes it to the slim-dev bundle through K3D_EXTRA_ARGS.
#
# The default flannel vxlan backend is not supported on some kernels
# ("failed to create vxlan device: operation not supported"). The extra
# arguments also select flannel host-gw, which is enough for a single-node
# cluster.
#
# Slim-dev installs Istio, Keycloak, and Pepr. It does not install the
# Portal layer. Adding tiles requires a later core-portal deploy on a host
# that has the memory for it.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${root}"

network_name="${UDS_K3D_NETWORK:-uds-k3d}"
cluster_name="${UDS_K3D_CLUSTER:-uds}"
# Keep this in lockstep with docs/operations/uds-package.md.
core_version="${UDS_CORE_VERSION:-1.17.1}"
core_ref="${UDS_CORE_REF:-oci://ghcr.io/defenseunicorns/packages/uds/bundles/k3d-core-slim-dev:${core_version}}"
dry_run=false
print_plan=false

usage() {
  cat <<EOF
Usage: $(basename "$0") [--dry-run] [--print-plan]

  --print-plan  Print the network, cluster, bundle, and k3d arguments, then exit.
  --dry-run     Create nothing. Print the commands that would run, then exit.

Environment:
  UDS_K3D_NETWORK   Docker network name (default: uds-k3d)
  UDS_K3D_CLUSTER   k3d cluster name the slim-dev bundle creates (default: uds)
  UDS_CORE_VERSION  Slim-dev bundle version (default: 1.17.1)
  UDS_CORE_REF      Full OCI reference. Overrides UDS_CORE_VERSION when set.
  K3D_EXTRA_ARGS    Replaces the default k3d extra arguments when set.
EOF
}

k3d_extra_args() {
  if [[ -n "${K3D_EXTRA_ARGS+x}" && -n "${K3D_EXTRA_ARGS}" ]]; then
    printf '%s' "${K3D_EXTRA_ARGS}"
    return
  fi
  printf '%s' "--network ${network_name} --k3s-arg --flannel-backend=host-gw@server:0"
}

print_plan_lines() {
  printf 'network=%s\n' "${network_name}"
  printf 'cluster=%s\n' "${cluster_name}"
  printf 'bundle=%s\n' "${core_ref}"
  printf 'k3d_extra_args=%s\n' "$(k3d_extra_args)"
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --print-plan) print_plan=true ;;
    --dry-run) dry_run=true ;;
    -h|--help) usage; exit 0 ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
  shift
done

if "${print_plan}"; then
  print_plan_lines
  exit 0
fi

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "$1 is required. Install the version pinned in docs/operations/uds-package.md." >&2
    exit 1
  fi
}

if "${dry_run}"; then
  print_plan_lines
  printf 'docker network inspect %s || docker network create %s\n' "${network_name}" "${network_name}"
  printf 'uds deploy %s --confirm --set K3D_EXTRA_ARGS=%s\n' "${core_ref}" "$(k3d_extra_args)"
  exit 0
fi

require_cmd docker
require_cmd uds

if ! docker info >/dev/null 2>&1; then
  echo "the Docker daemon is not reachable" >&2
  exit 1
fi

if ! docker network inspect "${network_name}" >/dev/null 2>&1; then
  docker network create "${network_name}"
fi

echo "Deploying ${core_ref}"
echo "K3D_EXTRA_ARGS=$(k3d_extra_args)"
uds deploy "${core_ref}" --confirm --set "K3D_EXTRA_ARGS=$(k3d_extra_args)"

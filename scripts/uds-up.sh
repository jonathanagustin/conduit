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
#
# After the bundle is deployed, the script applies two host adaptations
# that this kernel has required before: it clears hostPorts on the nginx
# DaemonSet (iptables multiport is missing on some hosts) and sets
# AMBIENT_ENABLED=false on Istio CNI (ambient has failed to initialize
# the host address set here).
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
  printf 'host_adaptations=clear-nginx-hostports,ambient-off\n'
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

kubectl_bin() {
  if command -v kubectl >/dev/null 2>&1; then
    printf '%s' kubectl
  else
    printf '%s' "uds zarf tools kubectl"
  fi
}

wait_for_api() {
  local k tries=0
  k="$(kubectl_bin)"
  if command -v k3d >/dev/null 2>&1; then
    k3d kubeconfig merge "${cluster_name}" --kubeconfig-merge-default >/dev/null || true
  fi
  while ! $k get ns >/dev/null 2>&1; do
    tries=$((tries + 1))
    if ((tries > 60)); then
      echo "the cluster API was not reachable after waiting" >&2
      return 1
    fi
    sleep 2
  done
}

clear_nginx_hostports() {
  local k json ns
  k="$(kubectl_bin)"
  ns=""
  if $k -n uds-dev-stack get ds nginx >/dev/null 2>&1; then
    ns=uds-dev-stack
  else
    ns="$($k get ds -A -o jsonpath='{range .items[?(@.metadata.name=="nginx")]}{.metadata.namespace}{"\n"}{end}' | head -1 || true)"
  fi
  if [[ -z "${ns}" ]]; then
    echo "nginx DaemonSet not found; skipping hostPort clear"
    return 0
  fi
  json="$($k -n "${ns}" get ds nginx -o json)"
  printf '%s' "${json}" | python3 -c '
import json, sys
doc = json.load(sys.stdin)
doc.pop("status", None)
doc.get("metadata", {}).pop("managedFields", None)
spec = doc.get("spec", {}).get("template", {}).get("spec", {})
for key in ("containers", "initContainers"):
    for container in spec.get(key) or []:
        for port in container.get("ports") or []:
            port.pop("hostPort", None)
json.dump(doc, sys.stdout)
' | $k apply -f -
  echo "Cleared hostPorts on daemonset/nginx in ${ns}"
}

disable_ambient() {
  local k
  k="$(kubectl_bin)"
  if $k -n istio-system get cm istio-cni-config >/dev/null 2>&1; then
    $k -n istio-system get cm istio-cni-config -o json | python3 -c '
import json, sys
doc = json.load(sys.stdin)
data = doc.setdefault("data", {})
# The ConfigMap may carry the flag as its own key or inside a file.
data["AMBIENT_ENABLED"] = "false"
for name, value in list(data.items()):
    if isinstance(value, str) and "AMBIENT_ENABLED" in value:
        data[name] = value.replace("AMBIENT_ENABLED=true", "AMBIENT_ENABLED=false")
json.dump(doc, sys.stdout)
' | $k apply -f -
    echo "Set AMBIENT_ENABLED=false on configmap/istio-cni-config"
  fi
  if $k -n istio-system get ds istio-cni >/dev/null 2>&1; then
    $k -n istio-system set env ds/istio-cni AMBIENT_ENABLED=false >/dev/null
    echo "Set AMBIENT_ENABLED=false on daemonset/istio-cni"
  fi
}

apply_host_adaptations() {
  wait_for_api
  clear_nginx_hostports
  disable_ambient
}


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
  printf 'apply host_adaptations=clear-nginx-hostports,ambient-off\n'
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
apply_host_adaptations

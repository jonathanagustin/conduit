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
source "${root}/scripts/lib/local-uds.sh"
conduit_uds_configure "${root}"

# Keep this in lockstep with docs/operations/uds-package.md.
core_version="${UDS_CORE_VERSION:-1.17.1}"
core_ref="${UDS_CORE_REF:-oci://ghcr.io/defenseunicorns/packages/uds/bundles/k3d-core-slim-dev:${core_version}}"
dry_run=false
print_plan=false

usage() {
  cat <<EOF
Usage: $(basename "$0") [--dry-run] [--print-plan]

  --print-plan  Print the network, cluster, bundle, and k3d arguments, then exit.
  --dry-run     Probe the engine and print commands; skip bundle-client checks.

Environment:
  CONDUIT_CONTAINER_ENGINE  Working Docker/Podman executable (auto-selected if unset)
  DOCKER_HOST      Docker API endpoint for UDS/k3d; required for Podman
  UDS_K3D_NETWORK   Network name (default: <cluster>-k3d; uds-k3d for uds)
  UDS_K3D_CLUSTER   k3d cluster name the slim-dev bundle creates (default: uds)
  UDS_KUBECONFIG    Absolute task-owned file (default: .cache/uds/<cluster>.kubeconfig)
  UDS_K3D_API_PORT  Optional bundle API port override (1-65535)
  UDS_K3D_HTTP_PORT Optional bundle HTTP port override (1-65535)
  UDS_K3D_HTTPS_PORT Optional bundle HTTPS port override (1-65535)
  UDS_CORE_VERSION  Slim-dev bundle version (default: 1.17.1)
  UDS_CORE_REF      Full OCI reference. Overrides UDS_CORE_VERSION; client preflight remains required.
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
  local kind variable
  printf 'network=%s\n' "${network_name}"
  printf 'cluster=%s\n' "${cluster_name}"
  printf 'bundle=%s\n' "${core_ref}"
  printf 'context=%s\n' "${kubecontext}"
  printf 'kubeconfig=%s\n' "${kubeconfig}"
  printf 'engine_request=%s\n' "${CONDUIT_CONTAINER_ENGINE:-auto}"
  printf 'bundle_client_requirement=working docker --version (checked before deployment)\n'
  printf 'k3d_extra_args=%s\n' "$(k3d_extra_args)"
  for kind in API HTTP HTTPS; do
    variable="UDS_K3D_${kind}_PORT"
    if [[ -n "${!variable:-}" ]]; then
      printf 'k3d_%s_port=%s\n' "${kind}" "${!variable}"
    fi
  done
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

wait_for_api() {
  local tries=0
  config_tmp="$(mktemp "${kubeconfig}.XXXXXX")"
  if ! k3d kubeconfig get "${cluster_name}" >"${config_tmp}"; then
    echo 'could not read the requested k3d cluster kubeconfig; host adaptations were not applied' >&2
    return 1
  fi
  if ! conduit_uds_assert_context "${config_tmp}"; then
    echo 'the requested k3d context was not selected; host adaptations were not applied' >&2
    return 1
  fi
  mv "${config_tmp}" "${kubeconfig}"
  config_tmp=''
  while ! conduit_uds_kubectl get ns >/dev/null 2>&1; do
    tries=$((tries + 1))
    if ((tries > 60)); then
      echo "the cluster API was not reachable after waiting" >&2
      return 1
    fi
    sleep 2
  done
}

clear_nginx_hostports() {
  local json ns
  ns=""
  if conduit_uds_kubectl -n uds-dev-stack get ds nginx >/dev/null 2>&1; then
    ns=uds-dev-stack
  else
    ns="$(conduit_uds_kubectl get ds -A -o jsonpath='{range .items[?(@.metadata.name=="nginx")]}{.metadata.namespace}{"\n"}{end}' | head -1 || true)"
  fi
  if [[ -z "${ns}" ]]; then
    echo "nginx DaemonSet not found; skipping hostPort clear"
    return 0
  fi
  json="$(conduit_uds_kubectl -n "${ns}" get ds nginx -o json)"
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
' | conduit_uds_kubectl apply -f -
  echo "Cleared hostPorts on daemonset/nginx in ${ns}"
}

disable_ambient() {
  if conduit_uds_kubectl -n istio-system get cm istio-cni-config >/dev/null 2>&1; then
    conduit_uds_kubectl -n istio-system get cm istio-cni-config -o json | python3 -c '
import json, sys
doc = json.load(sys.stdin)
data = doc.setdefault("data", {})
# The ConfigMap may carry the flag as its own key or inside a file.
data["AMBIENT_ENABLED"] = "false"
for name, value in list(data.items()):
    if isinstance(value, str) and "AMBIENT_ENABLED" in value:
        data[name] = value.replace("AMBIENT_ENABLED=true", "AMBIENT_ENABLED=false")
json.dump(doc, sys.stdout)
' | conduit_uds_kubectl apply -f -
    echo "Set AMBIENT_ENABLED=false on configmap/istio-cni-config"
  fi
  if conduit_uds_kubectl -n istio-system get ds istio-cni >/dev/null 2>&1; then
    conduit_uds_kubectl -n istio-system set env ds/istio-cni AMBIENT_ENABLED=false >/dev/null
    echo "Set AMBIENT_ENABLED=false on daemonset/istio-cni"
  fi
}

apply_host_adaptations() {
  wait_for_api
  clear_nginx_hostports
  disable_ambient
}


conduit_uds_bundle_args "$(k3d_extra_args)"
conduit_uds_engine
if "${dry_run}"; then
  print_plan_lines
  printf '%s network inspect %s || %s network create %s\n' "${engine}" "${network_name}" "${engine}" "${network_name}"
  printf 'uds deploy %s --confirm' "${core_ref}"
  printf ' %s' "${bundle_args[@]}"
  printf '\n'
  printf 'k3d kubeconfig get %s -> %s (context %s)\n' "${cluster_name}" "${kubeconfig}" "${kubecontext}"
  printf 'apply host_adaptations=clear-nginx-hostports,ambient-off\n'
  exit 0
fi

conduit_uds_require uds
conduit_uds_require k3d
# The pinned slim-dev bundle's airgap component calls docker directly to load
# images and create its image volume. A working Podman CLI is a separate tool.
# Keep this wrapper prerequisite with custom references; they are not qualified
# by overriding the reference and may have additional requirements.
conduit_host_tool_works docker --version || {
  echo 'uds-up requires a working Docker-compatible docker client on PATH for the pinned Core slim-dev airgap workflow (docker --version). The client may use DOCKER_HOST for the selected Podman API; a separate Docker daemon is not required.' >&2
  exit 1
}
conduit_host_tool_works python3 --version || { echo 'a working Python 3 is required for host adaptations' >&2; exit 1; }
conduit_uds_kubectl_command
umask 077
mkdir -p "$(dirname "${kubeconfig}")"
export KUBECONFIG="${kubeconfig}"
config_tmp=''
trap '[[ -z "${config_tmp}" ]] || rm -f "${config_tmp}"' EXIT
if [[ -s "${kubeconfig}" ]]; then
  conduit_uds_assert_context "${kubeconfig}" || { echo 'refusing deployment with an unverified task kubeconfig' >&2; exit 1; }
fi

if ! "${engine}" network inspect "${network_name}" >/dev/null 2>&1; then
  "${engine}" network create "${network_name}"
fi

echo "Deploying ${core_ref}"
echo "K3D_EXTRA_ARGS=$(k3d_extra_args)"
uds deploy "${core_ref}" --confirm "${bundle_args[@]}"
apply_host_adaptations

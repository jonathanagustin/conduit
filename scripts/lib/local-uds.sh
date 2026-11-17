#!/usr/bin/env bash
# Shared configuration for the local UDS lifecycle; never select an ambient
# Kubernetes context or infer a Docker API endpoint from a working CLI.
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/development-host.sh"

conduit_uds_configure() {
  local repository="$1" kind variable value
  cluster_name="${UDS_K3D_CLUSTER:-uds}"
  network_name="${UDS_K3D_NETWORK:-${cluster_name}-k3d}"
  kubeconfig="${UDS_KUBECONFIG:-${repository}/.cache/uds/${cluster_name}.kubeconfig}"
  kubecontext="k3d-${cluster_name}"
  if [[ ! "${cluster_name}" =~ ^[a-z0-9][a-z0-9-]*$ || ${#cluster_name} -gt 63 ]]; then
    echo 'UDS_K3D_CLUSTER must be a local cluster name containing lowercase letters, digits and hyphens, starting with a letter or digit (at most 63 characters)' >&2
    return 1
  fi
  if [[ ! "${network_name}" =~ ^[a-zA-Z0-9][a-zA-Z0-9_.-]*$ ]]; then
    echo 'UDS_K3D_NETWORK must be a network name, not arguments' >&2
    return 1
  fi
  if [[ "${kubeconfig}" != /* || "${kubeconfig}" == *:* || "${kubeconfig}" == *$'\n'* || "${kubeconfig}" == *$'\r'* ]]; then
    echo 'UDS_KUBECONFIG must name one absolute task-owned file, not a kubeconfig list' >&2
    return 1
  fi
  for kind in API HTTP HTTPS; do
    variable="UDS_K3D_${kind}_PORT"
    value="${!variable:-}"
    if [[ -n "${value}" ]] && { [[ ! "${value}" =~ ^[1-9][0-9]{0,4}$ ]] || ((value > 65535)); }; then
      echo "${variable} must be a port from 1 to 65535" >&2
      return 1
    fi
  done
}

# This array always contains the cluster and k3d arguments, including on the
# macOS system Bash where an empty array cannot be expanded under nounset.
conduit_uds_bundle_args() {
  local extra_args="$1" kind variable value
  bundle_args=(--set "CLUSTER_NAME=${cluster_name}" --set "K3D_EXTRA_ARGS=${extra_args}")
  for kind in API HTTP HTTPS; do
    variable="UDS_K3D_${kind}_PORT"
    value="${!variable:-}"
    if [[ -n "${value}" ]]; then
      bundle_args+=(--set "K3D_${kind}_PORT=${value}")
    fi
  done
}

conduit_uds_require() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "$1 is required. Install the version pinned in docs/operations/uds-package.md." >&2
    return 1
  }
}

conduit_uds_engine() {
  if [[ -n "${DOCKER_CONTEXT:-}" && -z "${DOCKER_HOST:-}" ]]; then
    echo 'UDS/k3d use the Docker API: set DOCKER_HOST for the selected local engine; a Docker CLI context alone does not select their endpoint' >&2
    return 1
  fi
  # Keep Docker CLI network operations on the explicitly chosen API, too.
  if [[ -n "${DOCKER_HOST:-}" ]]; then unset DOCKER_CONTEXT; fi
  engine="$(conduit_container_engine)" || return 1
  if [[ "${engine##*/}" != docker && -z "${DOCKER_HOST:-}" ]]; then
    echo 'UDS/k3d need DOCKER_HOST set to the selected engine Docker-compatible API endpoint; a working Podman CLI alone is insufficient. See docs/operations/local-uds-cluster.md.' >&2
    return 1
  fi
}

conduit_uds_kubectl_command() {
  if command -v kubectl >/dev/null 2>&1; then
    kubectl_command=(kubectl)
  else
    conduit_uds_require uds || return 1
    kubectl_command=(uds zarf tools kubectl)
  fi
}

conduit_uds_kubectl_at() {
  local config="$1"
  shift
  "${kubectl_command[@]}" --kubeconfig "${config}" --context "${kubecontext}" "$@"
}

conduit_uds_kubectl() {
  conduit_uds_kubectl_at "${kubeconfig}" "$@"
}

conduit_uds_assert_context() {
  local config="$1" selected listed
  selected="$(conduit_uds_kubectl_at "${config}" config current-context)" || {
    echo 'could not read the selected task kubeconfig context' >&2
    return 1
  }
  listed="$(conduit_uds_kubectl_at "${config}" config get-contexts "${kubecontext}" -o name)" || {
    echo 'could not verify the requested task kubeconfig context' >&2
    return 1
  }
  if [[ "${selected}" != "${kubecontext}" || "${listed}" != "${kubecontext}" ]]; then
    echo 'the task kubeconfig does not select the requested k3d context' >&2
    return 1
  fi
}

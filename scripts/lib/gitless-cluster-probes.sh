#!/usr/bin/env bash
# Sourced by the cluster owner; followups must finish before its EXIT cleanup.
run_gitless_cluster_probes() {
  local root="$1" tools="$2" kubeconfig="$3"
  [[ -r "${kubeconfig}" && -s "${kubeconfig}" ]] || { echo 'gitless probes: missing owning kubeconfig' >&2; return 1; }
  # A child cannot export PATH/KUBECONFIG back into the next workflow step.
  # Keep both probes here, in the same cluster's lifetime and explicit context.
  KUBECONFIG="${kubeconfig}" PATH="${tools}:${PATH}" GITLESS_STRICT_ACCEPTANCE=1 \
    "${root}/scripts/test-gitless-flux-manifests.sh" || return 1
  KUBECONFIG="${kubeconfig}" PATH="${tools}:${PATH}" \
    "${root}/scripts/test-gitless-acceptance.sh" || return 1
}

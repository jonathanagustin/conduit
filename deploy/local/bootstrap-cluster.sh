#!/usr/bin/env bash
set -euo pipefail

cluster_name="${PLATFORM_CLUSTER_NAME:-example}"
registry_name="${PLATFORM_REGISTRY_NAME:-example-registry}"
registry_port="${PLATFORM_REGISTRY_PORT:-5111}"

if ! k3d registry list --no-headers 2>/dev/null | awk '{print $1}' | grep -Fxq "k3d-${registry_name}"; then
  k3d registry create "${registry_name}" --port "${registry_port}"
fi

if ! k3d cluster list --no-headers 2>/dev/null | awk '{print $1}' | grep -Fxq "${cluster_name}"; then
  k3d cluster create "${cluster_name}" \
    --servers 1 \
    --agents 3 \
    --registry-use "k3d-${registry_name}:5000" \
    --k3s-arg '--disable=traefik@server:*' \
    --wait
fi

kubectl config use-context "k3d-${cluster_name}"
kubectl apply -f "$(dirname "$0")/prerequisites.yaml"
kubectl wait --for=condition=Ready nodes --all --timeout=180s

worker_count="$(kubectl get nodes -l '!node-role.kubernetes.io/control-plane' -o name | wc -l | tr -d ' ')"
if [[ "${worker_count}" -lt 3 ]]; then
  echo "expected at least three worker nodes, found ${worker_count}" >&2
  exit 1
fi

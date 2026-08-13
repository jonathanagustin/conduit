#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/../.." && pwd)"
registry_port="${PLATFORM_REGISTRY_PORT:-5111}"
host_registry="localhost:${registry_port}"
cluster_registry="k3d-${PLATFORM_REGISTRY_NAME:-example-registry}:5000"
release="${PLATFORM_DELIVERY_VERSION:-0.1.0}"

docker build -f "${repo_root}/Containerfile" \
  -t "${host_registry}/platform-delivery:${release}" "${repo_root}"
docker build -f "${repo_root}/Containerfile.agent" \
  -t "${host_registry}/platform-spoke-agent:${release}" "${repo_root}"
docker push "${host_registry}/platform-delivery:${release}"
docker push "${host_registry}/platform-spoke-agent:${release}"

delivery_digest="$(docker image inspect "${host_registry}/platform-delivery:${release}" --format '{{index .RepoDigests 0}}' | sed 's/.*@//')"
agent_digest="$(docker image inspect "${host_registry}/platform-spoke-agent:${release}" --format '{{index .RepoDigests 0}}' | sed 's/.*@//')"

sed \
  -e "s|\${DELIVERY_IMAGE}|${cluster_registry}/platform-delivery:${release}@${delivery_digest}|g" \
  -e "s|\${AGENT_IMAGE}|${cluster_registry}/platform-spoke-agent:${release}@${agent_digest}|g" \
  "${repo_root}/deploy/local/delivery-plane.yaml" | kubectl apply -f -

kubectl rollout status deployment/platform-delivery -n platform-system --timeout=180s
kubectl rollout status deployment/platform-spoke-agent -n platform-system --timeout=180s

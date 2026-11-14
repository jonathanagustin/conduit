#!/usr/bin/env bash
# Exercises scripts/uds-up.sh and scripts/uds-down.sh without creating a
# cluster. The plan output is the contract the bring-up task documents, so a
# change to the network name, bundle pin, or host-gw argument has to be
# deliberate.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
up="${root}/scripts/uds-up.sh"
down="${root}/scripts/uds-down.sh"

fail() {
  echo "$1" >&2
  exit 1
}

plan="$("${up}" --print-plan)"
printf '%s\n' "${plan}" | grep -qx -- 'network=uds-k3d' || fail "default network must be uds-k3d"
printf '%s\n' "${plan}" | grep -qx -- 'cluster=uds' || fail "default cluster must be uds"
printf '%s\n' "${plan}" | grep -qx -- 'bundle=oci://ghcr.io/defenseunicorns/packages/uds/bundles/k3d-core-slim-dev:1.17.1' ||
  fail "default bundle must stay pinned to slim-dev 1.17.1"
printf '%s\n' "${plan}" | grep -qx -- 'k3d_extra_args=--network uds-k3d --k3s-arg --flannel-backend=host-gw@server:0' ||
  fail "default k3d arguments must use the user-defined network and flannel host-gw"

override="$(UDS_K3D_NETWORK=custom-net UDS_CORE_VERSION=1.16.0 "${up}" --print-plan)"
printf '%s\n' "${override}" | grep -qx -- 'network=custom-net' || fail "UDS_K3D_NETWORK must change the network"
printf '%s\n' "${override}" | grep -qx -- 'bundle=oci://ghcr.io/defenseunicorns/packages/uds/bundles/k3d-core-slim-dev:1.16.0' ||
  fail "UDS_CORE_VERSION must change the bundle tag"
printf '%s\n' "${override}" | grep -F -q -- '--network custom-net' || fail "k3d extra arguments must follow the network override"

dry="$("${up}" --dry-run)"
printf '%s\n' "${dry}" | grep -q -- 'docker network inspect uds-k3d || docker network create uds-k3d' ||
  fail "dry-run must show the user-defined network create"
printf '%s\n' "${dry}" | grep -q -- 'uds deploy oci://ghcr.io/defenseunicorns/packages/uds/bundles/k3d-core-slim-dev:1.17.1 --confirm --set K3D_EXTRA_ARGS=--network uds-k3d --k3s-arg --flannel-backend=host-gw@server:0' ||
  fail "dry-run must show the slim-dev deploy with K3D_EXTRA_ARGS"

if "${up}" --print-plan | grep -Ei -- 'password|secret|token' >/dev/null; then
  fail "bring-up plan must not print credentials"
fi

down_plan="$("${down}" --print-plan)"
printf '%s\n' "${down_plan}" | grep -qx -- 'cluster=uds' || fail "uds-down default cluster must be uds"
printf '%s\n' "${down_plan}" | grep -qx -- 'network=uds-k3d' || fail "uds-down default network must be uds-k3d"
printf '%s\n' "${down_plan}" | grep -qx -- 'k3d cluster delete uds' || fail "uds-down must delete the k3d cluster"

if "${up}" --unknown >/tmp/uds-up-unknown-err 2>&1; then
  fail "unknown arguments must be rejected"
fi
grep -q 'unknown argument: --unknown' /tmp/uds-up-unknown-err || fail "unknown argument refusal must name the flag"
rm -f /tmp/uds-up-unknown-err

echo "UDS cluster bring-up plan verified"

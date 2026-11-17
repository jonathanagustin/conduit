#!/usr/bin/env bash
# Deploy one generated application package and request its Service.
#
# This is a manual check. The trunk Go lane is 20 minutes and already runs the
# race suite, the PostgreSQL gate, and the packaging gates. A cluster plus
# package initialization does not fit that budget, so the workflow does not
# call this script. See docs/operations/appgen-deploy.md.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${root}"

missing=()
for cmd in docker go zarf kubectl; do
  if ! command -v "${cmd}" >/dev/null 2>&1; then
    missing+=("${cmd}")
  fi
done
if ! command -v k3d >/dev/null 2>&1 && ! command -v kind >/dev/null 2>&1; then
  missing+=("k3d or kind")
fi
if ((${#missing[@]})); then
  echo "appgen deploy probe needs: ${missing[*]}" >&2
  exit 1
fi

export CONDUIT_APPGEN_DEPLOY=1
go test -count=1 -timeout 45m -run TestDeployGeneratedPackage -v ./internal/appgen/deployprobe

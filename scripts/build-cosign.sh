#!/usr/bin/env bash
# Build the host signing command from the same patched graph used by the Hub.
set -euo pipefail
[[ $# -eq 1 && "$1" = /* ]] || { echo 'usage: build-cosign.sh /absolute/output/cosign' >&2; exit 2; }
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export GOTOOLCHAIN=local
[[ "$(go env GOVERSION)" == go1.27.4 ]] || { echo 'Cosign source build requires the pinned Go 1.27.4 compiler' >&2; exit 1; }
mkdir -p "$(dirname -- "$1")"
CGO_ENABLED=0 GOOS="$(go env GOHOSTOS)" GOARCH="$(go env GOHOSTARCH)" \
  go -C "$root/tools/cosign-build" build -trimpath -buildvcs=false \
  -o "$1" github.com/sigstore/cosign/v3/cmd/cosign

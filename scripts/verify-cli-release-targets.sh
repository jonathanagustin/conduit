#!/usr/bin/env bash
# Proves the standalone release matrix compiles before an immutable tag is cut.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
output_root="$(mktemp -d)"
cleanup() {
  rm -rf -- "${output_root}"
}
trap cleanup EXIT

targets=(
  linux/amd64
  linux/arm64
  darwin/amd64
  darwin/arm64
  windows/amd64
  windows/arm64
)

cd "${repo_root}"
for target in "${targets[@]}"; do
  os="${target%/*}"
  architecture="${target#*/}"
  suffix=""
  if [[ "${os}" == windows ]]; then
    suffix='.exe'
  fi
  CGO_ENABLED=0 GOOS="${os}" GOARCH="${architecture}" go build \
    -trimpath -buildvcs=false \
    -o "${output_root}/conduit-${os}-${architecture}${suffix}" ./cmd/conduit
done

#!/usr/bin/env bash
# Idempotently stage the exact SBOM/vulnerability tools used by candidate scans.
# The cache and downloads are deliberately below /tmp; no credentials or repo
# state are written there. Explicit SYFT_BIN/GRYPE_BIN overrides remain caller-owned.

set -euo pipefail

platform_anchore_cache="${PLATFORM_SECURITY_TOOL_CACHE:-/tmp/platform-security-tools}"
case "$platform_anchore_cache" in
  /tmp/*) ;;
  *) printf 'security tool cache must be below /tmp\n' >&2; return 1 2>/dev/null || exit 1 ;;
esac

platform_anchore_architecture() {
  case "$(uname -m)" in
    x86_64) printf 'amd64\n' ;;
    aarch64|arm64) printf 'arm64\n' ;;
    *) printf 'unsupported host architecture: %s\n' "$(uname -m)" >&2; return 1 ;;
  esac
}

platform_anchore_tool() {
  local name="$1" version="$2" sha="$3" architecture="$4" target="$platform_anchore_cache/$1"
  local url="https://github.com/anchore/$name/releases/download/v$version/${name}_${version}_linux_${architecture}.tar.gz"
  mkdir -p "$platform_anchore_cache"
  if [ -x "$target" ] && "$target" version -o json 2>/dev/null | jq -e --arg name "$name" --arg version "$version" '.application == $name and .version == $version' >/dev/null; then
    printf '%s\n' "$target"
    return 0
  fi
  local work
  work="$(mktemp -d /tmp/platform-security-tool.XXXXXXXX)"
  trap 'rm -rf -- "$work"' RETURN
  curl --fail --location --silent --show-error "$url" --output "$work/$name.tar.gz"
  printf '%s  %s\n' "$sha" "$work/$name.tar.gz" | sha256sum --check --status
  tar -xzf "$work/$name.tar.gz" -C "$work" "$name"
  install -m 0755 "$work/$name" "$target"
  "$target" version -o json | jq -e --arg name "$name" --arg version "$version" '.application == $name and .version == $version' >/dev/null
  printf '%s\n' "$target"
}

platform_anchore_tools() {
  local architecture
  architecture="$(platform_anchore_architecture)"
  if [ -z "${SYFT_BIN:-}" ]; then
    case "$architecture" in
      amd64) SYFT_BIN="$(platform_anchore_tool syft 1.50.0 bf7b29ff57f06da30918266a0e1c2885a8f99784798d1bdb1628886aa015d788 "$architecture")" ;;
      arm64) SYFT_BIN="$(platform_anchore_tool syft 1.50.0 887c57cbcc2d0e8c5c110a4571a3fc7150058b24d74f993ee4663516e5c8ce86 "$architecture")" ;;
    esac
  fi
  if [ -z "${GRYPE_BIN:-}" ]; then
    case "$architecture" in
      amd64) GRYPE_BIN="$(platform_anchore_tool grype 0.116.1 0122df7b655981abe547ad3d2190d65551dac6a2bfc80b4dc2a989b5d0587458 "$architecture")" ;;
      arm64) GRYPE_BIN="$(platform_anchore_tool grype 0.116.1 a8d7504a149629324eb5f4ce3dc25dfd211bbfe047e64ee2bf7844b466c3d84d "$architecture")" ;;
    esac
  fi
  export SYFT_BIN GRYPE_BIN
  export GRYPE_DB_CACHE_DIR="${GRYPE_DB_CACHE_DIR:-/tmp/platform-grype-db}"
}

platform_anchore_tools

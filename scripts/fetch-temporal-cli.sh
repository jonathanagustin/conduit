#!/usr/bin/env bash
# Fetch the pinned native Temporal CLI. Existing callers use the stable
# .cache/temporal/temporal link; archives and executables are cached per host.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
version=1.10.0
case "$(uname -s)/$(uname -m)" in
  Linux/x86_64 | Linux/amd64)
    platform=linux_amd64
    sha=521f461f3d108902763bb71e85cd0fd382dc9357b59604ae03da6ae734d16890
    ;;
  Linux/aarch64 | Linux/arm64)
    platform=linux_arm64
    sha=6e526d2f089bf5acead2129f1f63e303b848410d5d9d86b9255359dcea6eaaa8
    ;;
  Darwin/x86_64 | Darwin/amd64)
    platform=darwin_amd64
    sha=150603ba135dc5f40f0a8f33b1a09732469692fae9a78e3024318e83f023136a
    ;;
  Darwin/arm64 | Darwin/aarch64)
    platform=darwin_arm64
    sha=40a76ef7d525e6195b083e4cd50b184cc3bb242fea7f8dda2b9a8698ecbb7379
    ;;
  *)
    echo "Unsupported Temporal CLI host: $(uname -s)/$(uname -m)" >&2
    exit 1
    ;;
esac
url="https://github.com/temporalio/cli/releases/download/v${version}/temporal_cli_${version}_${platform}.tar.gz"
if [[ "${1:-}" == --print-download ]]; then
  printf '%s  %s\n' "${sha}" "${url}"
  exit 0
fi
[[ "$#" -eq 0 ]] || { echo 'usage: fetch-temporal-cli.sh [--print-download]' >&2; exit 2; }

dest="${CONDUIT_TEMPORAL_CACHE:-${root}/.cache/temporal}"
host_dir="${dest}/${platform}"
archive="${host_dir}/temporal_cli_${version}_${platform}.tar.gz"
mkdir -p "${host_dir}"

verify_archive() {
  if command -v sha256sum >/dev/null 2>&1; then
    printf '%s  %s\n' "${sha}" "$1" | sha256sum -c - >/dev/null
  elif command -v shasum >/dev/null 2>&1; then
    printf '%s  %s\n' "${sha}" "$1" | shasum -a 256 -c - >/dev/null
  else
    echo 'sha256sum or shasum is required to verify the Temporal CLI archive' >&2
    return 1
  fi
}

temporary="$(mktemp -d "${host_dir}/.fetch.XXXXXX")"
trap 'rm -rf "${temporary}"' EXIT
if [[ ! -f "${archive}" ]] || ! verify_archive "${archive}"; then
  curl -fsSL --retry 3 --retry-delay 2 -o "${temporary}/archive.tar.gz" "${url}"
  verify_archive "${temporary}/archive.tar.gz"
  mv -f "${temporary}/archive.tar.gz" "${archive}"
fi
# Extract only the expected executable from the verified archive. Refreshing
# it also repairs a stale or modified executable without trusting the cache.
tar -xzf "${archive}" -C "${temporary}" temporal
"${temporary}/temporal" --version
mv -f "${temporary}/temporal" "${host_dir}/temporal"
ln -s "${platform}/temporal" "${temporary}/link"
mv -f "${temporary}/link" "${dest}/temporal"

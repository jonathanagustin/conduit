#!/usr/bin/env bash
# Stage the exact flat attachment set before authenticating its checksum index.
set -euo pipefail
fail() { printf 'assemble-release-assets: %s\n' "$*" >&2; exit 1; }
[[ $# -ge 2 && -d "$1" ]] || fail 'usage: assemble-release-assets.sh <empty-directory> <asset>...'
output="$1"
shift
shopt -s nullglob dotglob
existing=("${output}"/*)
[[ ${#existing[@]} -eq 0 ]] || fail 'output directory must be empty'
for source in "$@"; do
  name="${source##*/}"
  [[ "${name}" =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ && "${name}" != release-assets.sha256* ]] ||
    fail "invalid or reserved asset name: ${name}"
  [[ -f "${source}" && ! -L "${source}" && -s "${source}" ]] || fail "missing or empty asset: ${source}"
  [[ ! -e "${output}/${name}" ]] || fail "duplicate asset name: ${name}"
  cp "${source}" "${output}/${name}"
done
(cd "${output}" && sha256sum * > release-assets.sha256)

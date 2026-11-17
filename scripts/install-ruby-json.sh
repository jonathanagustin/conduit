#!/usr/bin/env bash
# Install the fixed JSON gem reviewed in THIRD-PARTY-TRUST-REVIEW.md.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
patched_json() {
  ruby -rjson -rrubygems/version -e 'exit(Gem::Version.new(JSON::VERSION) >= Gem::Version.new("2.19.9") ? 0 : 1)'
}
if patched_json >/dev/null 2>&1; then
  echo 'Ruby JSON already meets the patched version floor'
  exit 0
fi

sha=9b9025b7cdddafa38d316eca0b2358488e42d417045c1b90d216a9fefe46b79a
cache="${MISE_CACHE_DIR:-${root}/.cache/mise}"
mkdir -p "${cache}"
temporary="$(mktemp -d "${cache}/ruby-json.XXXXXX")"
trap 'rm -rf "${temporary}"' EXIT
gem_file="${temporary}/json-2.19.9.gem"
curl -fsSL --retry 3 --retry-delay 2 -o "${gem_file}" https://rubygems.org/downloads/json-2.19.9.gem
if command -v sha256sum >/dev/null 2>&1; then
  printf '%s  %s\n' "${sha}" "${gem_file}" | sha256sum -c -
elif command -v shasum >/dev/null 2>&1; then
  printf '%s  %s\n' "${sha}" "${gem_file}" | shasum -a 256 -c -
else
  echo 'sha256sum or shasum is required to verify the JSON gem' >&2
  exit 1
fi
ruby -S gem install --local "${gem_file}" --no-document
patched_json

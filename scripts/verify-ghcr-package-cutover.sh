#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "usage: GH_TOKEN=<token> $0 <owner/repository> <package> <package-id>" >&2
  exit 2
}

[[ "$#" -eq 3 ]] || usage
[[ -n "${GH_TOKEN:-}" ]] || {
  echo "GH_TOKEN is required" >&2
  exit 2
}

target_repository="$1"
package="$2"
expected_package_id="$3"
owner="${target_repository%%/*}"
[[ -n "${owner}" && "${target_repository}" == */* && -n "${target_repository#*/}" ]] || usage
[[ "${expected_package_id}" =~ ^[0-9]+$ ]] || usage

api_url="${GH_API_URL:-https://api.github.com}"
authorization_header="Authorization: Bearer ${GH_TOKEN}"
accept_header="Accept: application/vnd.github+json"
version_header="X-GitHub-Api-Version: 2022-11-28"

repository_json="$(curl --fail-with-body --silent --show-error \
  --header "${authorization_header}" \
  --header "${accept_header}" \
  --header "${version_header}" \
  "${api_url}/repos/${target_repository}")"
package_json="$(curl --fail-with-body --silent --show-error \
  --header "${authorization_header}" \
  --header "${accept_header}" \
  --header "${version_header}" \
  "${api_url}/users/${owner}/packages/container/${package}")"

repository_id="$(jq -er '.id' <<<"${repository_json}")"
package_id="$(jq -er '.id' <<<"${package_json}")"
visibility="$(jq -er '.visibility' <<<"${package_json}")"
linked_repository="$(jq -er '.repository.full_name' <<<"${package_json}")"
linked_repository_id="$(jq -er '.repository.id' <<<"${package_json}")"

[[ "${package_id}" == "${expected_package_id}" ]] || {
  echo "${package}: package identity changed: expected ${expected_package_id}, observed ${package_id}" >&2
  exit 1
}
[[ "${visibility}" == "private" ]] || {
  echo "${package}: visibility changed: expected private, observed ${visibility}" >&2
  exit 1
}
[[ "${linked_repository}" == "${target_repository}" ]] || {
  echo "${package}: linked to ${linked_repository}, expected ${target_repository}" >&2
  exit 1
}
[[ "${linked_repository_id}" == "${repository_id}" ]] || {
  echo "${package}: linked repository ID ${linked_repository_id} does not match ${target_repository} ID ${repository_id}" >&2
  exit 1
}

echo "${package}: private package ${package_id} is linked to ${target_repository} (${repository_id})"

#!/usr/bin/env bash
# Clean-client verification of a published or staged Conduit release directory
# (#844).
#
# Checks that every release manifest, CLI archive, signature bundle, SLSA
# provenance bundle, SPDX SBOM, Helm chart package, and air-gap Zarf package
# is present, internally consistent with the release version and commit, and
# matches the complete asset index signed by the release workflow. This checks
# downloaded attachments; registry availability and deployment are separate.
set -euo pipefail

fail() {
  printf 'verify-published-release: %s\n' "$*" >&2
  exit 1
}

release_dir="${1:-}"
version="${RELEASE_VERSION:-$(tr -d '[:space:]' < "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/VERSION")}"
commit="${RELEASE_COMMIT:-${GITHUB_SHA:-}}"
repository="${GITHUB_REPOSITORY:-jonathanagustin/conduit}"
cosign_bin="${COSIGN_BIN:-cosign}"
expected_issuer="${RELEASE_CERTIFICATE_ISSUER:-https://token.actions.githubusercontent.com}"
expected_identity="${RELEASE_CERTIFICATE_IDENTITY:-https://github.com/${repository}/.github/workflows/platform-images.yml@refs/tags/v${version}}"

[[ -n "${release_dir}" && -d "${release_dir}" ]] ||
  fail "usage: verify-published-release.sh <release-assets-dir> (got '${release_dir:-<unset>}')"
[[ "${version}" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] ||
  fail "RELEASE_VERSION must be three-part SemVer without leading v; got '${version}'"
command -v "${cosign_bin}" >/dev/null 2>&1 || fail "Cosign is required: ${cosign_bin}"

inventory_checked=false
asset_inventory='|'

require_file() {
  [[ -f "${release_dir}/$1" && ! -L "${release_dir}/$1" && -s "${release_dir}/$1" ]] ||
    fail "missing or empty release asset: $1"
  if [[ "${inventory_checked}" == true && "${asset_inventory}" != *"|$1|"* ]]; then
    fail "release asset is not in the signed inventory: $1"
  fi
}

# Never let checksum contents choose paths outside the downloaded directory.
# The publisher uses sha256sum's text format and flat, unique asset names.
check_checksums() {
  local index="$1" expected_count="${2:-0}" expected_name="${3:-}"
  local line hash name actual count=0 names='|'
  while IFS= read -r line || [[ -n "${line}" ]]; do
    [[ "${line}" =~ ^([a-f0-9]{64})\ \ ([A-Za-z0-9][A-Za-z0-9._-]*)$ ]] ||
      fail "invalid checksum record in ${index}"
    hash="${BASH_REMATCH[1]}"
    name="${BASH_REMATCH[2]}"
    [[ "${name}" != release-assets.sha256* && "${names}" != *"|${name}|"* ]] ||
      fail "duplicate or reserved checksum asset in ${index}: ${name}"
    [[ -z "${expected_name}" || "${name}" == "${expected_name}" ]] || fail "unexpected checksum asset in ${index}: ${name}"
    require_file "${name}"
    actual="$(sha256sum "${release_dir}/${name}" | awk '{print $1}')"
    [[ "${actual}" == "${hash}" ]] || fail "${name} SHA-256 disagrees with ${index}"
    names="${names}${name}|"
    count=$((count + 1))
  done < "${release_dir}/${index}"
  [[ "${count}" -gt 0 && ( "${expected_count}" -eq 0 || "${count}" -eq "${expected_count}" ) ]] ||
    fail "unexpected checksum asset count in ${index}: ${count}"
  if [[ "${index}" == release-assets.sha256 ]]; then asset_inventory="${names}"; fi
}

require_file release-assets.sha256
require_file release-assets.sha256.sigstore.json
"${cosign_bin}" verify-blob \
  --bundle "${release_dir}/release-assets.sha256.sigstore.json" \
  --certificate-identity "${expected_identity}" \
  --certificate-oidc-issuer "${expected_issuer}" \
  "${release_dir}/release-assets.sha256" >/dev/null || fail 'signed release inventory verification failed'
check_checksums release-assets.sha256
inventory_checked=true
shopt -s nullglob dotglob
for asset_path in "${release_dir}"/*; do
  asset_name="${asset_path##*/}"
  case "${asset_name}" in release-assets.sha256|release-assets.sha256.sigstore.json) continue ;; esac
  require_file "${asset_name}"
done

require_file "platform-oci-manifest.json"
require_file "platform-runtime.json"
require_file "platform-helm-charts.json"
require_file "SHA256SUMS"

manifest_version="$(jq -er '.version' "${release_dir}/platform-oci-manifest.json")"
[[ "${manifest_version}" == "${version}" ]] ||
  fail "platform-oci-manifest.json version '${manifest_version}' disagrees with '${version}'"
jq -e '.apiVersion == "platform.delivery/oci-release/v2" and
  (.images | keys == ["hub", "spoke-agent"])' "${release_dir}/platform-oci-manifest.json" >/dev/null ||
  fail 'unsupported image manifest schema or components'
manifest_commit="$(jq -er '.commit' "${release_dir}/platform-oci-manifest.json")"
[[ "${manifest_commit}" =~ ^[a-f0-9]{40}$ ]] || fail 'image manifest commit must be a full Git SHA'

if [[ -n "${commit}" ]]; then
  [[ "${manifest_commit}" == "${commit}" ]] ||
    fail "platform-oci-manifest.json commit '${manifest_commit}' disagrees with '${commit}'"
fi
commit="${manifest_commit}"

for component in hub spoke-agent; do
  ref="$(jq -er --arg c "${component}" '.images[$c].reference' "${release_dir}/platform-oci-manifest.json")"
  [[ "${ref}" =~ ^ghcr\.io/${repository}@sha256:[a-f0-9]{64}$ ]] ||
    fail "component ${component} reference '${ref}' is not a digest-pinned repository image"
  jq -e --arg c "${component}" '.images[$c] |
    .reference == (.image + "@" + .digest) and
    ([.sboms[].architecture] | sort == ["amd64", "arm64"]) and
    ([.vulnerabilityReports[].architecture] | sort == ["amd64", "arm64"])' \
    "${release_dir}/platform-oci-manifest.json" >/dev/null || fail "inconsistent image descriptor: ${component}"
  for arch in amd64 arm64; do
    require_file "${component}-${arch}.spdx.json"
    require_file "${component}-${arch}.syft.json"
    require_file "${component}-${arch}.grype.json"
    sbom_sha="$(sha256sum "${release_dir}/${component}-${arch}.spdx.json" | awk '{print $1}')"
    recorded_sha="$(jq -er --arg c "${component}" --arg a "${arch}" \
      '.images[$c].sboms[] | select(.architecture == $a) | .sha256' \
      "${release_dir}/platform-oci-manifest.json")"
    [[ "${sbom_sha}" == "${recorded_sha}" ]] ||
      fail "${component}-${arch}.spdx.json SHA-256 ${sbom_sha} disagrees with manifest ${recorded_sha}"
    report_sha="$(sha256sum "${release_dir}/${component}-${arch}.grype.json" | awk '{print $1}')"
    recorded_sha="$(jq -er --arg c "${component}" --arg a "${arch}" \
      '.images[$c].vulnerabilityReports[] | select(.architecture == $a) | .sha256' \
      "${release_dir}/platform-oci-manifest.json")"
    [[ "${report_sha}" == "${recorded_sha}" ]] || fail "${component}-${arch}.grype.json SHA-256 disagrees with manifest"
  done
done

runtime_ref="$(jq -er '.artifact.reference' "${release_dir}/platform-runtime.json")"
[[ "${runtime_ref}" =~ ^ghcr\.io/${repository}:runtime-${version}@sha256:[a-f0-9]{64}$ ]] ||
  fail "platform-runtime.json reference '${runtime_ref}' is not digest-pinned for version ${version}"
jq -e --arg v "${version}" --arg c "${commit}" --arg r "${repository}" \
  --arg i "${expected_identity}" --arg issuer "${expected_issuer}" \
  '.apiVersion == "conduit.delivery/runtime-source/v1" and .kind == "RuntimeSource" and
   .version == $v and .source == {repository: $r, commit: $c} and
   .artifact.digest == $digest and
   .signaturePolicy == {provider: "cosign", certificateIssuer: $issuer, certificateIdentity: $i}' \
  --arg digest "${runtime_ref##*@}" "${release_dir}/platform-runtime.json" >/dev/null || fail 'inconsistent runtime descriptor'

helm_version="$(jq -er '.version' "${release_dir}/platform-helm-charts.json")"
[[ "${helm_version}" == "${version}" ]] ||
  fail "platform-helm-charts.json version '${helm_version}' disagrees with '${version}'"
jq -e --arg c "${commit}" --arg v "${version}" \
  '.apiVersion == "platform.delivery/helm-release/v1" and .commit == $c and
   ([.charts[].component] | sort == ["hub", "spoke"]) and
   all(.charts[]; .commit == $c and .version == $v and .digest == (.reference | split("@") | .[1]))' \
  "${release_dir}/platform-helm-charts.json" >/dev/null || fail 'inconsistent Helm manifest'

for component in hub spoke; do
  chart="platform-${component}-${version}.tgz"
  require_file "${chart}"
  require_file "${chart}.sha256"
  require_file "${chart}.spdx.json"
  check_checksums "${chart}.sha256" 1 "${chart}"
  chart_ref="$(jq -er --arg c "${component}" '.charts[] | select(.component == $c) | .reference' "${release_dir}/platform-helm-charts.json")"
  [[ "${chart_ref}" =~ ^ghcr\.io/${repository}/platform-${component}:${version}@sha256:[a-f0-9]{64}$ ]] ||
    fail "Helm chart ${component} reference '${chart_ref}' is not digest-pinned"
  spdx_ver="$(jq -er '.packages[0].versionInfo' "${release_dir}/${chart}.spdx.json")"
  [[ "${spdx_ver}" == "${version}" ]] ||
    fail "${chart}.spdx.json versionInfo '${spdx_ver}' disagrees with '${version}'"
done

for arch in amd64 arm64; do
  pkg="zarf-package-platform-runtime-${arch}-${version}.tar.zst"
  require_file "${pkg}"
  require_file "${pkg}.sha256"
  check_checksums "${pkg}.sha256" 1 "${pkg}"
done

check_checksums SHA256SUMS 6

targets=(linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64)
sum_lines="$(grep -c . "${release_dir}/SHA256SUMS")"
[[ "${sum_lines}" -eq "${#targets[@]}" ]] ||
  fail "SHA256SUMS must list exactly ${#targets[@]} CLI archives, found ${sum_lines}"

for target in "${targets[@]}"; do
  os="${target%/*}"
  arch="${target#*/}"
  ext="tar.gz"
  [[ "${os}" == "windows" ]] && ext="zip"
  base="conduit_${version}_${os}_${arch}"
  archive="${base}.${ext}"
  require_file "${archive}"
  require_file "${archive}.sigstore.json"
  require_file "${archive}.provenance.sigstore.json"
  require_file "${base}.slsa.json"
  require_file "${base}.spdx.json"
  awk -v name="${archive}" '$2 == name {found++} END {exit found != 1}' "${release_dir}/SHA256SUMS" ||
    fail "SHA256SUMS does not index ${archive}"
  slsa_ver="$(jq -er '.buildDefinition.externalParameters.version' "${release_dir}/${base}.slsa.json")"
  [[ "${slsa_ver}" == "${version}" ]] ||
    fail "${base}.slsa.json version '${slsa_ver}' disagrees with '${version}'"
  "${cosign_bin}" verify-blob \
    --bundle "${release_dir}/${archive}.sigstore.json" \
    --certificate-identity "${expected_identity}" \
    --certificate-oidc-issuer "${expected_issuer}" \
    "${release_dir}/${archive}" >/dev/null ||
    fail "cosign verify-blob failed for ${archive}"
  "${cosign_bin}" verify-blob-attestation \
    --bundle "${release_dir}/${archive}.provenance.sigstore.json" \
    --type slsaprovenance1 \
    --certificate-identity "${expected_identity}" \
    --certificate-oidc-issuer "${expected_issuer}" \
    "${release_dir}/${archive}" >/dev/null ||
    fail "cosign verify-blob-attestation failed for ${archive}"
done

printf 'verified published release assets for v%s in %s\n' "${version}" "${release_dir}"

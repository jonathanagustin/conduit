#!/usr/bin/env bash
# Clean-client verification of a published or staged Conduit release directory
# (#844).
#
# Checks that every release manifest, CLI archive, signature bundle, SLSA
# provenance bundle, SPDX SBOM, Helm chart package, and air-gap Zarf package
# is present, internally consistent with the release version and commit, and
# matches its SHA-256 index and Cosign provenance bundle.
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

require_file() {
  [[ -s "${release_dir}/$1" ]] || fail "missing or empty release asset: $1"
}

require_file "platform-oci-manifest.json"
require_file "platform-runtime.json"
require_file "platform-helm-charts.json"
require_file "SHA256SUMS"

manifest_version="$(jq -er '.version' "${release_dir}/platform-oci-manifest.json")"
[[ "${manifest_version}" == "${version}" ]] ||
  fail "platform-oci-manifest.json version '${manifest_version}' disagrees with '${version}'"

if [[ -n "${commit}" ]]; then
  manifest_commit="$(jq -er '.commit' "${release_dir}/platform-oci-manifest.json")"
  [[ "${manifest_commit}" == "${commit}" ]] ||
    fail "platform-oci-manifest.json commit '${manifest_commit}' disagrees with '${commit}'"
fi

for component in hub spoke-agent; do
  ref="$(jq -er --arg c "${component}" '.images[] | select(.component == $c) | .reference' "${release_dir}/platform-oci-manifest.json")"
  [[ "${ref}" =~ ^ghcr\.io/${repository}@sha256:[a-f0-9]{64}$ ]] ||
    fail "component ${component} reference '${ref}' is not a digest-pinned repository image"
  for arch in amd64 arm64; do
    require_file "${component}-${arch}.spdx.json"
    require_file "${component}-${arch}.syft.json"
    require_file "${component}-${arch}.grype.json"
    sbom_sha="$(sha256sum "${release_dir}/${component}-${arch}.spdx.json" | awk '{print $1}')"
    recorded_sha="$(jq -er --arg c "${component}" --arg a "${arch}" \
      '.images[] | select(.component == $c) | .sboms[] | select(.architecture == $a) | .sha256' \
      "${release_dir}/platform-oci-manifest.json")"
    [[ "${sbom_sha}" == "${recorded_sha}" ]] ||
      fail "${component}-${arch}.spdx.json SHA-256 ${sbom_sha} disagrees with manifest ${recorded_sha}"
  done
done

runtime_ref="$(jq -er '.reference' "${release_dir}/platform-runtime.json")"
[[ "${runtime_ref}" =~ ^ghcr\.io/${repository}:runtime-${version}@sha256:[a-f0-9]{64}$ ]] ||
  fail "platform-runtime.json reference '${runtime_ref}' is not digest-pinned for version ${version}"

helm_version="$(jq -er '.version' "${release_dir}/platform-helm-charts.json")"
[[ "${helm_version}" == "${version}" ]] ||
  fail "platform-helm-charts.json version '${helm_version}' disagrees with '${version}'"

for component in hub spoke; do
  chart="platform-${component}-${version}.tgz"
  require_file "${chart}"
  require_file "${chart}.sha256"
  require_file "${chart}.spdx.json"
  (cd "${release_dir}" && sha256sum --check "${chart}.sha256" >/dev/null) ||
    fail "Helm chart archive ${chart} failed SHA-256 verification"
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
  (cd "${release_dir}" && sha256sum --check "${pkg}.sha256" >/dev/null) ||
    fail "Zarf package ${pkg} failed SHA-256 verification"
done

(cd "${release_dir}" && sha256sum --check SHA256SUMS >/dev/null) ||
  fail "CLI archive SHA256SUMS check failed"

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
  grep -qE "^[a-f0-9]{64}[[:space:]]+${archive}$" "${release_dir}/SHA256SUMS" ||
    fail "SHA256SUMS does not index ${archive}"
  slsa_ver="$(jq -er '.buildDefinition.externalParameters.version' "${release_dir}/${base}.slsa.json")"
  [[ "${slsa_ver}" == "${version}" ]] ||
    fail "${base}.slsa.json version '${slsa_ver}' disagrees with '${version}'"
  if command -v "${cosign_bin}" >/dev/null 2>&1; then
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
  fi
done

printf 'verified published release assets for v%s in %s\n' "${version}" "${release_dir}"

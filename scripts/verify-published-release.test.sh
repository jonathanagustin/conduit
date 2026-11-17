#!/usr/bin/env bash
# Proves verify-published-release.sh accepts a complete release asset set and
# refuses each class of missing, corrupted, or version-drifted asset (#844).
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-published-release.sh"
test_dir="$(mktemp -d)"
trap 'rm -rf "${test_dir}"' EXIT

version="0.18.0"
commit="$(printf 'a%.0s' {1..40})"
digest="$(printf 'b%.0s' {1..64})"

mkdir -p "${test_dir}/bin"
cat > "${test_dir}/bin/cosign" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
if [[ -f "${FAKE_COSIGN_FAIL:-}" ]]; then
  echo "cosign verification failed" >&2
  exit 1
fi
printf '%s\n' "$*" >> "${FAKE_COSIGN_LOG}"
[[ "$*" == *"--certificate-identity https://github.com/jonathanagustin/conduit/.github/workflows/platform-images.yml@refs/tags/v0.18.0"* ]]
[[ "$*" == *"--certificate-oidc-issuer https://token.actions.githubusercontent.com"* ]]
exit 0
STUB
chmod 0755 "${test_dir}/bin/cosign"
export FAKE_COSIGN_LOG="${test_dir}/cosign.log"

seal_fixture() {
  local dir="$1" staged
  rm -f "${dir}/release-assets.sha256" "${dir}/release-assets.sha256.sigstore.json"
  staged="$(mktemp -d "${test_dir}/staged.XXXXXX")"
  "${repo_root}/scripts/assemble-release-assets.sh" "${staged}" "${dir}"/*
  mv "${staged}/release-assets.sha256" "${dir}/release-assets.sha256"
  # Cryptography is the explicit boundary stubbed by this offline test.
  printf '{"bundle":"inventory"}\n' > "${dir}/release-assets.sha256.sigstore.json"
  rm -rf "${staged}"
}

build_fixture() {
  local dir="$1"
  rm -rf "${dir}"
  mkdir -p "${dir}"

  for component in hub spoke-agent; do
    for arch in amd64 arm64; do
      printf '{"spdxVersion":"SPDX-2.3","component":"%s","arch":"%s"}\n' "${component}" "${arch}" > "${dir}/${component}-${arch}.spdx.json"
      printf '{"descriptor":{"name":"syft"}}\n' > "${dir}/${component}-${arch}.syft.json"
      printf '{"descriptor":{"name":"grype"}}\n' > "${dir}/${component}-${arch}.grype.json"
    done
  done

  hub_amd64_sha="$(sha256sum "${dir}/hub-amd64.spdx.json" | awk '{print $1}')"
  hub_arm64_sha="$(sha256sum "${dir}/hub-arm64.spdx.json" | awk '{print $1}')"
  agent_amd64_sha="$(sha256sum "${dir}/spoke-agent-amd64.spdx.json" | awk '{print $1}')"
  agent_arm64_sha="$(sha256sum "${dir}/spoke-agent-arm64.spdx.json" | awk '{print $1}')"

  jq -n \
    --arg v "${version}" \
    --arg c "${commit}" \
    --arg d "sha256:${digest}" \
    --arg h1 "${hub_amd64_sha}" \
    --arg h2 "${hub_arm64_sha}" \
    --arg a1 "${agent_amd64_sha}" \
    --arg a2 "${agent_arm64_sha}" \
    '{
      apiVersion: "platform.delivery/oci-release/v1",
      version: $v,
      commit: $c,
      images: [
        {
          component: "hub",
          reference: ("ghcr.io/jonathanagustin/conduit@" + $d),
          digest: $d,
          sboms: [
            {architecture: "amd64", file: "hub-amd64.spdx.json", sha256: $h1},
            {architecture: "arm64", file: "hub-arm64.spdx.json", sha256: $h2}
          ]
        },
        {
          component: "spoke-agent",
          reference: ("ghcr.io/jonathanagustin/conduit@" + $d),
          digest: $d,
          sboms: [
            {architecture: "amd64", file: "spoke-agent-amd64.spdx.json", sha256: $a1},
            {architecture: "arm64", file: "spoke-agent-arm64.spdx.json", sha256: $a2}
          ]
        }
      ]
    }' > "${dir}/platform-oci-manifest.json"

  # Exercise the publisher, rather than a hand-written approximation of its
  # schema. Its v2 manifest keys images by component and omits .component.
  for component in hub spoke-agent; do
    jq --arg component "${component}" \
      '. as $manifest | .images[] | select(.component == $component) |
       . + {image: "ghcr.io/jonathanagustin/conduit", version: $manifest.version,
            commit: $manifest.commit}' \
      "${dir}/platform-oci-manifest.json" > "${dir}/${component}.json"
    for arch in amd64 arm64; do
      report_sha="$(sha256sum "${dir}/${component}-${arch}.grype.json" | awk '{print $1}')"
      jq --arg arch "${arch}" --arg file "${component}-${arch}.grype.json" --arg sha "${report_sha}" \
        '.vulnerabilityReports += [{architecture: $arch, file: $file, sha256: $sha}]' \
        "${dir}/${component}.json" > "${dir}/descriptor.tmp"
      mv "${dir}/descriptor.tmp" "${dir}/${component}.json"
    done
  done
  "${repo_root}/scripts/assemble-oci-manifest.sh" "${dir}/platform-oci-manifest.json" \
    "${dir}/hub.json" "${dir}/spoke-agent.json"
  rm "${dir}/hub.json" "${dir}/spoke-agent.json"

  "${NODE_BIN:-node}" "${repo_root}/scripts/render-runtime-source-descriptor.mjs" \
    --reference "ghcr.io/jonathanagustin/conduit:runtime-${version}@sha256:${digest}" \
    --digest "sha256:${digest}" --version "${version}" --commit "${commit}" \
    --source-repository jonathanagustin/conduit \
    --certificate-issuer https://token.actions.githubusercontent.com \
    --certificate-identity "https://github.com/jonathanagustin/conduit/.github/workflows/platform-images.yml@refs/tags/v${version}" \
    > "${dir}/platform-runtime.json"

  for component in hub spoke; do
    chart="platform-${component}-${version}.tgz"
    printf 'chart-%s-%s\n' "${component}" "${version}" > "${dir}/${chart}"
    (cd "${dir}" && sha256sum "${chart}" > "${chart}.sha256")
    jq -n --arg v "${version}" '{packages: [{versionInfo: $v}]}' > "${dir}/${chart}.spdx.json"
  done

  jq -n --arg v "${version}" --arg d "sha256:${digest}" --arg c "${commit}" \
    '{
      apiVersion: "platform.delivery/helm-release/v1",
      version: $v,
      commit: $c,
      charts: [
        {component: "hub", version: $v, commit: $c, digest: $d, reference: ("ghcr.io/jonathanagustin/conduit/platform-hub:" + $v + "@" + $d)},
        {component: "spoke", version: $v, commit: $c, digest: $d, reference: ("ghcr.io/jonathanagustin/conduit/platform-spoke:" + $v + "@" + $d)}
      ]
    }' > "${dir}/platform-helm-charts.json"

  for arch in amd64 arm64; do
    pkg="zarf-package-platform-runtime-${arch}-${version}.tar.zst"
    printf 'zarf-%s-%s\n' "${arch}" "${version}" > "${dir}/${pkg}"
    (cd "${dir}" && sha256sum "${pkg}" > "${pkg}.sha256")
  done

  : > "${dir}/SHA256SUMS"
  for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64; do
    os="${target%/*}"
    arch="${target#*/}"
    ext="tar.gz"
    [[ "${os}" == "windows" ]] && ext="zip"
    base="conduit_${version}_${os}_${arch}"
    archive="${base}.${ext}"
    printf 'cli-%s\n' "${base}" > "${dir}/${archive}"
    printf '{"bundle":"sig"}\n' > "${dir}/${archive}.sigstore.json"
    printf '{"bundle":"prov"}\n' > "${dir}/${archive}.provenance.sigstore.json"
    jq -n --arg v "${version}" '{buildDefinition: {externalParameters: {version: $v}}}' > "${dir}/${base}.slsa.json"
    printf '{"spdxVersion":"SPDX-2.3"}\n' > "${dir}/${base}.spdx.json"
  done
  (cd "${dir}" && sha256sum conduit_*.tar.gz conduit_*.zip | sort -k2 > SHA256SUMS)
  seal_fixture "${dir}"
}

run_verifier() {
  COSIGN_BIN="${TEST_COSIGN_BIN:-${test_dir}/bin/cosign}" \
    RELEASE_VERSION="${version}" \
    RELEASE_COMMIT="${commit}" \
    GITHUB_REPOSITORY="jonathanagustin/conduit" \
    "${verifier}" "$1"
}

expect_failure() {
  local name="$1" expected="$2"
  shift 2
  local dir="${test_dir}/${name}"
  build_fixture "${dir}"
  "$@" "${dir}"
  if [[ "${RESEAL_FIXTURE:-false}" == true ]]; then seal_fixture "${dir}"; fi
  local out
  if out="$(run_verifier "${dir}" 2>&1)"; then
    printf 'expected verify-published-release.sh to refuse %s\n' "${name}" >&2
    exit 1
  fi
  grep -qF "${expected}" <<<"${out}" || {
    printf 'refusal for %s did not contain %q: %s\n' "${name}" "${expected}" "${out}" >&2
    exit 1
  }
}

expect_signed_failure() { RESEAL_FIXTURE=true expect_failure "$@"; }

good="${test_dir}/good"
build_fixture "${good}"
run_verifier "${good}" >/dev/null
[[ "$(wc -l < "${FAKE_COSIGN_LOG}" | tr -d ' ')" -eq 13 ]] || {
  echo 'expected inventory verification and signature plus provenance for all six CLI archives' >&2
  exit 1
}
head -n 1 "${FAKE_COSIGN_LOG}" | grep -qF 'release-assets.sha256.sigstore.json'

expect_failure "missing_manifest" "missing or empty release asset: platform-oci-manifest.json" \
  bash -c 'rm -f "$1/platform-oci-manifest.json"' _
expect_signed_failure "version_drift" "platform-oci-manifest.json version '0.17.5' disagrees with '0.18.0'" \
  bash -c 'jq ".version = \"0.17.5\"" "$1/platform-oci-manifest.json" > "$1/tmp" && mv "$1/tmp" "$1/platform-oci-manifest.json"' _
expect_signed_failure "unpinned_image" "is not a digest-pinned repository image" \
  bash -c 'jq ".images.hub.reference = \"ghcr.io/jonathanagustin/conduit:latest\"" "$1/platform-oci-manifest.json" > "$1/tmp" && mv "$1/tmp" "$1/platform-oci-manifest.json"' _
expect_failure "corrupted_sbom" "hub-amd64.spdx.json SHA-256" \
  bash -c 'echo tampered >> "$1/hub-amd64.spdx.json"' _
expect_failure "corrupted_native_sbom" "hub-amd64.syft.json SHA-256 disagrees with release-assets.sha256" \
  bash -c 'echo tampered >> "$1/hub-amd64.syft.json"' _
expect_failure "corrupted_chart" "platform-hub-0.18.0.tgz SHA-256" \
  bash -c 'echo tampered >> "$1/platform-hub-0.18.0.tgz"' _
expect_failure "corrupted_cli" "conduit_0.18.0_linux_amd64.tar.gz SHA-256" \
  bash -c 'echo tampered >> "$1/conduit_0.18.0_linux_amd64.tar.gz"' _
expect_failure "missing_provenance" "missing or empty release asset: conduit_0.18.0_linux_amd64.tar.gz.provenance.sigstore.json" \
  bash -c 'rm -f "$1/conduit_0.18.0_linux_amd64.tar.gz.provenance.sigstore.json"' _

touch "${test_dir}/fail-cosign"
export FAKE_COSIGN_FAIL="${test_dir}/fail-cosign"
expect_failure "cosign_rejected" "signed release inventory verification failed" true
unset FAKE_COSIGN_FAIL

expect_failure "missing_inventory" "missing or empty release asset: release-assets.sha256" \
  bash -c 'rm "$1/release-assets.sha256"' _
expect_failure "missing_inventory_signature" "missing or empty release asset: release-assets.sha256.sigstore.json" \
  bash -c 'rm "$1/release-assets.sha256.sigstore.json"' _
TEST_COSIGN_BIN="${test_dir}/missing-cosign" expect_failure "missing_cosign" "Cosign is required" true
expect_failure "unindexed_extra" "release asset is not in the signed inventory: extra.json" \
  bash -c 'echo extra > "$1/extra.json"' _
expect_failure "joined_inventory_names" "invalid release asset name" \
  bash -c 'first=$(sed -n "1s/^[^ ]*  //p" "$1/release-assets.sha256"); second=$(sed -n "2s/^[^ ]*  //p" "$1/release-assets.sha256"); printf unsigned > "$1/$first|$second"' _
expect_failure "unindexed_required" "release asset is not in the signed inventory: platform-runtime.json" \
  bash -c 'sed "/  platform-runtime.json$/d" "$1/release-assets.sha256" > "$1/new-index"; mv "$1/new-index" "$1/release-assets.sha256"' _
expect_failure "traversal_index" "invalid checksum record in release-assets.sha256" \
  bash -c 'printf "%064d  ../outside\n" 0 >> "$1/release-assets.sha256"' _
expect_failure "duplicate_index" "duplicate or reserved checksum asset" \
  bash -c 'head -n 1 "$1/release-assets.sha256" >> "$1/release-assets.sha256"' _
expect_signed_failure "image_digest_mismatch" "inconsistent image descriptor: hub" \
  bash -c 'jq ".images.hub.digest = \"sha256:bad\"" "$1/platform-oci-manifest.json" > "$1/tmp"; mv "$1/tmp" "$1/platform-oci-manifest.json"' _
expect_signed_failure "runtime_commit_mismatch" "inconsistent runtime descriptor" \
  bash -c 'jq ".source.commit = \"bad\"" "$1/platform-runtime.json" > "$1/tmp"; mv "$1/tmp" "$1/platform-runtime.json"' _
expect_signed_failure "runtime_identity_mismatch" "inconsistent runtime descriptor" \
  bash -c 'jq ".signaturePolicy.certificateIdentity = \"https://attacker.invalid/\"" "$1/platform-runtime.json" > "$1/tmp"; mv "$1/tmp" "$1/platform-runtime.json"' _
expect_signed_failure "helm_commit_mismatch" "inconsistent Helm manifest" \
  bash -c 'jq ".charts[0].commit = \"bad\"" "$1/platform-helm-charts.json" > "$1/tmp"; mv "$1/tmp" "$1/platform-helm-charts.json"' _
expect_signed_failure "corrupted_report" "hub-amd64.grype.json SHA-256 disagrees with manifest" \
  bash -c 'echo tampered >> "$1/hub-amd64.grype.json"' _

mkdir "${test_dir}/duplicates"
if "${repo_root}/scripts/assemble-release-assets.sh" "${test_dir}/duplicates" \
  "${good}/SHA256SUMS" "${good}/SHA256SUMS" >"${test_dir}/duplicate.log" 2>&1; then
  echo 'expected duplicate attachment basename refusal' >&2
  exit 1
fi
grep -qF 'duplicate asset name: SHA256SUMS' "${test_dir}/duplicate.log"

echo "verify-published-release: all cases passed"

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
exit 0
STUB
chmod 0755 "${test_dir}/bin/cosign"

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

  jq -n --arg v "${version}" --arg d "sha256:${digest}" \
    '{reference: ("ghcr.io/jonathanagustin/conduit:runtime-" + $v + "@" + $d), version: $v}' \
    > "${dir}/platform-runtime.json"

  for component in hub spoke; do
    chart="platform-${component}-${version}.tgz"
    printf 'chart-%s-%s\n' "${component}" "${version}" > "${dir}/${chart}"
    (cd "${dir}" && sha256sum "${chart}" > "${chart}.sha256")
    jq -n --arg v "${version}" '{packages: [{versionInfo: $v}]}' > "${dir}/${chart}.spdx.json"
  done

  jq -n --arg v "${version}" --arg d "sha256:${digest}" \
    '{
      apiVersion: "platform.delivery/helm-release/v1",
      version: $v,
      charts: [
        {component: "hub", reference: ("ghcr.io/jonathanagustin/conduit/platform-hub:" + $v + "@" + $d)},
        {component: "spoke", reference: ("ghcr.io/jonathanagustin/conduit/platform-spoke:" + $v + "@" + $d)}
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
}

run_verifier() {
  COSIGN_BIN="${test_dir}/bin/cosign" \
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

good="${test_dir}/good"
build_fixture "${good}"
run_verifier "${good}" >/dev/null

expect_failure "missing_manifest" "missing or empty release asset: platform-oci-manifest.json" \
  bash -c 'rm -f "$1/platform-oci-manifest.json"' _
expect_failure "version_drift" "platform-oci-manifest.json version '0.17.5' disagrees with '0.18.0'" \
  bash -c 'jq ".version = \"0.17.5\"" "$1/platform-oci-manifest.json" > "$1/tmp" && mv "$1/tmp" "$1/platform-oci-manifest.json"' _
expect_failure "unpinned_image" "is not a digest-pinned repository image" \
  bash -c 'jq ".images[0].reference = \"ghcr.io/jonathanagustin/conduit:latest\"" "$1/platform-oci-manifest.json" > "$1/tmp" && mv "$1/tmp" "$1/platform-oci-manifest.json"' _
expect_failure "corrupted_sbom" "hub-amd64.spdx.json SHA-256" \
  bash -c 'echo tampered >> "$1/hub-amd64.spdx.json"' _
expect_failure "corrupted_chart" "Helm chart archive platform-hub-0.18.0.tgz failed SHA-256" \
  bash -c 'echo tampered >> "$1/platform-hub-0.18.0.tgz"' _
expect_failure "corrupted_cli" "CLI archive SHA256SUMS check failed" \
  bash -c 'echo tampered >> "$1/conduit_0.18.0_linux_amd64.tar.gz"' _
expect_failure "missing_provenance" "missing or empty release asset: conduit_0.18.0_linux_amd64.tar.gz.provenance.sigstore.json" \
  bash -c 'rm -f "$1/conduit_0.18.0_linux_amd64.tar.gz.provenance.sigstore.json"' _

touch "${test_dir}/fail-cosign"
export FAKE_COSIGN_FAIL="${test_dir}/fail-cosign"
expect_failure "cosign_rejected" "cosign verify-blob failed" true
unset FAKE_COSIGN_FAIL

echo "verify-published-release: all cases passed"

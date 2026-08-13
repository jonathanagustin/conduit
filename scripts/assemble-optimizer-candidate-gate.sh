#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'assemble-optimizer-candidate-gate: %s\n' "$*" >&2
  exit 1
}

[[ $# -eq 5 ]] || fail 'usage: assemble-optimizer-candidate-gate.sh BACKEND PLATFORM_REVISION AMD64_ARTIFACT_ROOT ARM64_ARTIFACT_ROOT NEW_OUTPUT'

backend="$1"
revision="$2"
amd64_root="$3"
arm64_root="$4"
output="$5"

case "$backend" in
  highs) upstream_version='1.15.1' ;;
  cpsat) upstream_version='9.15.6755' ;;
  *) fail 'backend must be highs or cpsat' ;;
esac
[[ "$revision" =~ ^[0-9a-f]{40}$ ]] || fail 'Platform revision must be a full lowercase Git SHA-1'
[[ ! -e "$output" ]] || fail 'output already exists'

for command_name in git jq sha256sum stat cmp mktemp; do
  command -v "$command_name" >/dev/null 2>&1 || fail "required executable is unavailable: $command_name"
done

repository_root="$(git rev-parse --show-toplevel)"
[[ "$(git -C "$repository_root" rev-parse HEAD)" == "$revision" ]] ||
  fail 'checked-out source does not match the requested Platform revision'
git -C "$repository_root" diff --quiet --ignore-submodules -- ||
  fail 'checked-out source has unstaged changes'
git -C "$repository_root" diff --cached --quiet --ignore-submodules -- ||
  fail 'checked-out source has staged changes'
[[ -z "$(git -C "$repository_root" ls-files --others --exclude-standard)" ]] ||
  fail 'checked-out source has untracked files'
source_tree_sha="$(git -C "$repository_root" ls-tree -r -z --full-tree "$revision" | sha256sum | cut -d' ' -f1)"

declare -A candidate_metadata security_metadata semantic_response image_archive
declare -A reproducibility candidate_sha security_sha semantic_sha image_archive_sha image_archive_bytes reproducibility_sha
declare -A image_id binary_sha request_sha spdx_sha scan_sha

require_regular_file() {
  local path="$1"
  [[ -f "$path" && ! -L "$path" ]] || fail "required regular file is absent or symbolic: $path"
}

read_architecture() {
  local architecture="$1"
  local root="$2"
  local candidate_dir security_dir spdx scan recorded_candidate_sha recorded_spdx_sha recorded_scan_sha recorded_semantic_sha

  candidate_dir="$root/${backend}-${architecture}"
  security_dir="$root/${backend}-${architecture}-security"
  candidate_metadata[$architecture]="$candidate_dir/candidate-metadata.json"
  security_metadata[$architecture]="$security_dir/security-metadata.json"
  semantic_response[$architecture]="$candidate_dir/semantic-response.json"
  image_archive[$architecture]="$root/${backend}-${architecture}-image.tar.gz"
  reproducibility[$architecture]="$root/${backend}-${architecture}-reproducibility.json"
  spdx="$security_dir/${backend}-${architecture}.spdx.json"
  scan="$security_dir/${backend}-${architecture}.grype.json"

  require_regular_file "${candidate_metadata[$architecture]}"
  require_regular_file "${security_metadata[$architecture]}"
  require_regular_file "${semantic_response[$architecture]}"
  require_regular_file "${image_archive[$architecture]}"
  require_regular_file "${reproducibility[$architecture]}"
  require_regular_file "$spdx"
  require_regular_file "$scan"

  jq -e --arg architecture "$architecture" --arg revision "$revision" '
    .schemaVersion == 1 and .admission == "UNADMITTED" and
    .architecture == $architecture and .platformRevision == $revision and
    (.imageId | test("^sha256:[0-9a-f]{64}$")) and
    (.binarySha256 | test("^[0-9a-f]{64}$")) and
    (.semanticResponseSha256 | test("^[0-9a-f]{64}$"))
  ' "${candidate_metadata[$architecture]}" >/dev/null ||
    fail "candidate metadata is invalid or not UNADMITTED for $architecture"

  jq -e --arg architecture "$architecture" --arg revision "$revision" '
    .schemaVersion == 1 and .admission == "UNADMITTED" and
    .architecture == $architecture and .platformRevision == $revision and
    (.candidateMetadataSha256 | test("^[0-9a-f]{64}$")) and
    .tools.syft == "1.50.0" and .tools.grype == "0.116.1" and
    .vulnerability.grypeExitStatus == 0 and
    .vulnerability.database.valid == true and
    (.sbom.spdxSha256 | test("^[0-9a-f]{64}$")) and
    (.vulnerability.reportSha256 | test("^[0-9a-f]{64}$"))
  ' "${security_metadata[$architecture]}" >/dev/null ||
    fail "security evidence is invalid or does not pass the high-severity gate for $architecture"

  candidate_sha[$architecture]="$(sha256sum "${candidate_metadata[$architecture]}" | cut -d' ' -f1)"
  security_sha[$architecture]="$(sha256sum "${security_metadata[$architecture]}" | cut -d' ' -f1)"
  semantic_sha[$architecture]="$(sha256sum "${semantic_response[$architecture]}" | cut -d' ' -f1)"
  image_archive_sha[$architecture]="$(sha256sum "${image_archive[$architecture]}" | cut -d' ' -f1)"
  reproducibility_sha[$architecture]="$(sha256sum "${reproducibility[$architecture]}" | cut -d' ' -f1)"
  image_archive_bytes[$architecture]="$(stat -c '%s' "${image_archive[$architecture]}")"
  image_id[$architecture]="$(jq -er '.imageId' "${candidate_metadata[$architecture]}")"
  binary_sha[$architecture]="$(jq -er '.binarySha256' "${candidate_metadata[$architecture]}")"
  request_sha[$architecture]="$(jq -r '.requestSha256 // ""' "${candidate_metadata[$architecture]}")"
  recorded_candidate_sha="$(jq -er '.candidateMetadataSha256' "${security_metadata[$architecture]}")"
  spdx_sha[$architecture]="$(sha256sum "$spdx" | cut -d' ' -f1)"
  scan_sha[$architecture]="$(sha256sum "$scan" | cut -d' ' -f1)"
  recorded_spdx_sha="$(jq -er '.sbom.spdxSha256' "${security_metadata[$architecture]}")"
  recorded_scan_sha="$(jq -er '.vulnerability.reportSha256' "${security_metadata[$architecture]}")"

  jq -e --arg backend "$backend" --arg architecture "$architecture" \
    --arg revision "$revision" --arg imageId "${image_id[$architecture]}" '
      .apiVersion == "optimization.platform.example/optimizer-reproducibility/v1alpha1" and
      .kind == "OptimizerCandidateReproducibility" and .admission == "UNADMITTED" and
      .backend == $backend and .architecture == $architecture and
      .platformRevision == $revision and .cachePruned == true and .noCache == true and
      .firstImageId == $imageId and .secondImageId == $imageId and
      .byteIdenticalImage == true and .registryMutated == false and
      .optimizerStateMutated == false
    ' "${reproducibility[$architecture]}" >/dev/null ||
    fail "cache-independent rebuild evidence is invalid for $architecture"

  [[ "${candidate_sha[$architecture]}" == "$recorded_candidate_sha" ]] ||
    fail "security evidence does not bind candidate metadata for $architecture"
  [[ "${semantic_sha[$architecture]}" == "$(jq -er '.semanticResponseSha256' "${candidate_metadata[$architecture]}")" ]] ||
    fail "semantic response does not match candidate metadata for $architecture"
  [[ "${spdx_sha[$architecture]}" == "$recorded_spdx_sha" ]] ||
    fail "SPDX evidence does not match security metadata for $architecture"
  [[ "${scan_sha[$architecture]}" == "$recorded_scan_sha" ]] ||
    fail "vulnerability report does not match security metadata for $architecture"
  recorded_semantic_sha="$(jq -r '.semanticResponseSha256 // ""' "${security_metadata[$architecture]}")"
  [[ -z "$recorded_semantic_sha" || "$recorded_semantic_sha" == "${semantic_sha[$architecture]}" ]] ||
    fail "security evidence has a foreign semantic response for $architecture"
  [[ "${image_archive_bytes[$architecture]}" =~ ^[1-9][0-9]*$ ]] ||
    fail "candidate image archive is empty for $architecture"
}

read_architecture amd64 "$amd64_root"
read_architecture arm64 "$arm64_root"

[[ "${semantic_sha[amd64]}" == "${semantic_sha[arm64]}" ]] ||
  fail 'candidate semantic responses differ across architectures'
cmp "${semantic_response[amd64]}" "${semantic_response[arm64]}" >/dev/null ||
  fail 'candidate semantic response bytes differ across architectures'
[[ "$(jq -Sc '.tools' "${security_metadata[amd64]}")" == "$(jq -Sc '.tools' "${security_metadata[arm64]}")" ]] ||
  fail 'security tool identities differ across architectures'
[[ "$(jq -r '.vulnerability.database.source' "${security_metadata[amd64]}")" == \
   "$(jq -r '.vulnerability.database.source' "${security_metadata[arm64]}")" ]] ||
  fail 'vulnerability database sources differ across architectures'

if [[ "$backend" == highs ]]; then
  [[ "${request_sha[amd64]}" =~ ^[0-9a-f]{64}$ ]] || fail 'HiGHS request digest is absent'
  [[ "${request_sha[amd64]}" == "${request_sha[arm64]}" ]] ||
    fail 'HiGHS request digests differ across architectures'
else
  [[ -z "${request_sha[amd64]}" && -z "${request_sha[arm64]}" ]] ||
    fail 'CP-SAT candidate unexpectedly carries the HiGHS request identity'
fi

mkdir -p "$(dirname "$output")"
temporary="$(mktemp "${output}.tmp.XXXXXXXX")"
cleanup() {
  [[ -n "${temporary:-}" && -f "$temporary" ]] && rm -f -- "$temporary"
}
trap cleanup EXIT

jq --null-input --sort-keys \
  --arg apiVersion 'optimization.platform.example/optimizer-candidate-gate/v1alpha1' \
  --arg backend "$backend" \
  --arg upstreamVersion "$upstream_version" \
  --arg platformRevision "$revision" \
  --arg sourceTreeDigest "sha256:${source_tree_sha}" \
  --arg semanticResponseSha256 "${semantic_sha[amd64]}" \
  --arg requestSha256 "${request_sha[amd64]}" \
  --arg amd64ImageId "${image_id[amd64]}" \
  --arg arm64ImageId "${image_id[arm64]}" \
  --arg amd64BinarySha256 "${binary_sha[amd64]}" \
  --arg arm64BinarySha256 "${binary_sha[arm64]}" \
  --arg amd64CandidateMetadataSha256 "${candidate_sha[amd64]}" \
  --arg arm64CandidateMetadataSha256 "${candidate_sha[arm64]}" \
  --arg amd64SecurityMetadataSha256 "${security_sha[amd64]}" \
  --arg arm64SecurityMetadataSha256 "${security_sha[arm64]}" \
  --arg amd64ImageArchiveSha256 "${image_archive_sha[amd64]}" \
  --arg arm64ImageArchiveSha256 "${image_archive_sha[arm64]}" \
  --arg amd64ReproducibilitySha256 "${reproducibility_sha[amd64]}" \
  --arg arm64ReproducibilitySha256 "${reproducibility_sha[arm64]}" \
  --argjson amd64ImageArchiveBytes "${image_archive_bytes[amd64]}" \
  --argjson arm64ImageArchiveBytes "${image_archive_bytes[arm64]}" \
  '{apiVersion: $apiVersion, kind: "OptimizerCandidateGate", admission: "UNADMITTED",
    selectable: false, backend: $backend, upstreamVersion: $upstreamVersion,
    platformRevision: $platformRevision, sourceTreeDigest: $sourceTreeDigest,
    requestSha256: (if $requestSha256 == "" then null else $requestSha256 end),
    semanticResponseSha256: $semanticResponseSha256,
    architectures: {
      amd64: {imageId: $amd64ImageId, binarySha256: $amd64BinarySha256,
        candidateMetadataSha256: $amd64CandidateMetadataSha256,
        securityMetadataSha256: $amd64SecurityMetadataSha256,
        reproducibilitySha256: $amd64ReproducibilitySha256,
        imageArchiveSha256: $amd64ImageArchiveSha256,
        imageArchiveBytes: $amd64ImageArchiveBytes},
      arm64: {imageId: $arm64ImageId, binarySha256: $arm64BinarySha256,
        candidateMetadataSha256: $arm64CandidateMetadataSha256,
        securityMetadataSha256: $arm64SecurityMetadataSha256,
        reproducibilitySha256: $arm64ReproducibilitySha256,
        imageArchiveSha256: $arm64ImageArchiveSha256,
        imageArchiveBytes: $arm64ImageArchiveBytes}},
    gates: {nativeMultiArchitecture: true, exactSemanticEquivalence: true,
      cacheIndependentRebuild: true, nativeCandidateReceiptsBound: true,
      boundedSecurityEvidence: true},
    registryMutated: false, optimizerStateMutated: false,
    limitation: "This gate does not publish, admit, enable, select, approve, lease, or execute a recommendation."}' \
  > "$temporary"

chmod 0444 "$temporary"
mv "$temporary" "$output"
trap - EXIT
printf 'candidate_gate=pass\nbackend=%s\nplatform_revision=%s\nevidence=%s\n' "$backend" "$revision" "$output"

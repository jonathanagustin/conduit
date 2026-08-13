#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'scan-cpsat-candidate: %s\n' "$*" >&2
  exit 1
}

[[ $# -eq 3 ]] || fail 'usage: scan-cpsat-candidate.sh IMAGE CANDIDATE_EVIDENCE_DIRECTORY NEW_OUTPUT_DIRECTORY'

image="$1"
candidate_evidence_dir="$2"
output_dir="$3"
candidate_metadata="$candidate_evidence_dir/candidate-metadata.json"
static_closure="$candidate_evidence_dir/static-closure.tsv"
source "$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)/ensure-anchore-tools.sh"
syft_bin="$SYFT_BIN"
grype_bin="$GRYPE_BIN"

[[ -s "$candidate_metadata" ]] || fail 'candidate metadata is required'
[[ -s "$static_closure" ]] || fail 'compiler-owned static closure is required'
[[ ! -e "$output_dir" ]] || fail 'output directory must not already exist'
for command_name in "$syft_bin" "$grype_bin" docker jq sha256sum timeout mktemp; do
  command -v "$command_name" >/dev/null 2>&1 || fail "required executable is unavailable: $command_name"
done

syft_version_json="$($syft_bin version -o json)"
grype_version_json="$($grype_bin version -o json)"
[[ "$(jq -er '.application + ":" + .version' <<<"$syft_version_json")" == 'syft:1.50.0' ]] \
  || fail 'Syft must be exactly version 1.50.0'
[[ "$(jq -er '.application + ":" + .version' <<<"$grype_version_json")" == 'grype:0.116.1' ]] \
  || fail 'Grype must be exactly version 0.116.1'

architecture="$(jq -er '.architecture' "$candidate_metadata")"
case "$architecture" in
  amd64 | arm64) ;;
  *) fail 'candidate metadata has an unsupported architecture' ;;
esac
candidate_image_id="$(jq -er '.imageId' "$candidate_metadata")"
candidate_revision="$(jq -er '.platformRevision' "$candidate_metadata")"
static_closure_sha="$(jq -er '.staticClosureSha256' "$candidate_metadata")"
static_closure_entries="$(jq -er '.staticClosureEntries' "$candidate_metadata")"
semantic_sha="$(jq -er '.semanticResponseSha256' "$candidate_metadata")"
runtime_base_digest="$(jq -er '.runtimeBaseDigest' "$candidate_metadata")"
[[ "$static_closure_sha" =~ ^[0-9a-f]{64}$ ]] || fail 'candidate static-closure digest is invalid'
[[ "$semantic_sha" =~ ^[0-9a-f]{64}$ ]] || fail 'candidate semantic digest is invalid'
[[ "$static_closure_entries" =~ ^[0-9]+$ && "$static_closure_entries" -gt 0 ]] \
  || fail 'candidate static-closure entry count is invalid'
[[ "$(sha256sum "$static_closure" | cut -d' ' -f1)" == "$static_closure_sha" ]] \
  || fail 'compiler-owned static closure differs from candidate metadata'
[[ "$(awk 'NR > 1 { count++ } END { print count + 0 }' "$static_closure")" == "$static_closure_entries" ]] \
  || fail 'compiler-owned static closure count differs from candidate metadata'

inspect_json="$(docker image inspect "$image")"
[[ "$(jq -er '.[0].Id' <<<"$inspect_json")" == "$candidate_image_id" ]] \
  || fail 'candidate evidence does not bind the scanned image ID'
[[ "$(jq -er '.[0].Architecture' <<<"$inspect_json")" == "$architecture" ]] \
  || fail 'candidate evidence does not bind the scanned architecture'
[[ "$(jq -er '.[0].Config.Labels["io.palantir.platform.optimizer.admission"]' <<<"$inspect_json")" == 'UNADMITTED' ]] \
  || fail 'only an explicitly UNADMITTED candidate may use this scanner'
[[ "$(jq -er '.[0].Config.Labels["org.opencontainers.image.revision"]' <<<"$inspect_json")" == "$candidate_revision" ]] \
  || fail 'candidate evidence does not bind the scanned Platform revision'
[[ "$(jq -er '.[0].Config.Labels["io.palantir.platform.optimizer.runtime.base.digest"]' <<<"$inspect_json")" == "$runtime_base_digest" ]] \
  || fail 'candidate evidence does not bind the scanned runtime base'

mkdir -m 0755 "$output_dir"
native_sbom="$output_dir/cpsat-${architecture}.syft.json"
spdx_sbom="$output_dir/cpsat-${architecture}.spdx.json"
scan_report="$output_dir/cpsat-${architecture}.grype.json"
scan_metadata="$output_dir/security-metadata.json"

"$syft_bin" scan "docker:${image}" --quiet --platform "linux/${architecture}" \
  --output "syft-json=${native_sbom}" --output "spdx-json=${spdx_sbom}"
jq -e --arg architecture "$architecture" --arg image "$image" '
  .descriptor.name == "syft" and .descriptor.version == "1.50.0" and
  .source.type == "image" and .source.metadata.architecture == $architecture and
  .source.metadata.userInput == $image and
  .source.metadata.labels["io.palantir.platform.optimizer.admission"] == "UNADMITTED" and
  (.artifacts | type) == "array" and (.files | type) == "array"
' "$native_sbom" >/dev/null || fail 'Syft evidence does not bind the candidate image'
jq -e '
  .spdxVersion == "SPDX-2.3" and .dataLicense == "CC0-1.0" and
  (.creationInfo.creators | index("Tool: syft-1.50.0")) != null
' "$spdx_sbom" >/dev/null || fail 'SPDX evidence is not the required Syft 1.50.0 document'

grype_config="$(mktemp -t cpsat-grype.XXXXXXXX.yaml)"
cleanup() {
  if [[ -f "$grype_config" && "$grype_config" == /tmp/cpsat-grype.*.yaml ]]; then
    rm -f -- "$grype_config"
  fi
}
trap cleanup EXIT

set +e
GRYPE_DB_AUTO_UPDATE=false GRYPE_DB_REQUIRE_UPDATE_CHECK=false \
  timeout 10m "$grype_bin" "sbom:${native_sbom}" --config "$grype_config" --quiet --by-cve \
  --output json --file "$scan_report" --fail-on high
scan_status="$?"
set -e
[[ "$scan_status" -eq 0 || "$scan_status" -eq 2 ]] || fail "Grype failed with status ${scan_status}"
jq -e '
  .descriptor.name == "grype" and .descriptor.version == "0.116.1" and
  .descriptor.db.status.valid == true and .source.type == "image"
' "$scan_report" >/dev/null || fail 'Grype evidence is invalid or has no valid database provenance'

candidate_metadata_sha="$(sha256sum "$candidate_metadata" | cut -d' ' -f1)"
native_sbom_sha="$(sha256sum "$native_sbom" | cut -d' ' -f1)"
spdx_sbom_sha="$(sha256sum "$spdx_sbom" | cut -d' ' -f1)"
scan_report_sha="$(sha256sum "$scan_report" | cut -d' ' -f1)"
syft_image_id="$(jq -er '.source.metadata.imageID' "$native_sbom")"
manifest_digest="$(jq -er '.source.metadata.manifestDigest' "$native_sbom")"
package_count="$(jq -er '.artifacts | length' "$native_sbom")"
match_count="$(jq -er '.matches | length' "$scan_report")"
database_schema="$(jq -er '.descriptor.db.status.schemaVersion' "$scan_report")"
database_built="$(jq -er '.descriptor.db.status.built' "$scan_report")"
database_source="$(jq -er '.descriptor.db.status.from' "$scan_report")"
database_valid="$(jq -er '.descriptor.db.status.valid' "$scan_report")"

jq --null-input \
  --arg architecture "$architecture" \
  --arg imageId "$candidate_image_id" \
  --arg syftImageId "$syft_image_id" \
  --arg manifestDigest "$manifest_digest" \
  --arg platformRevision "$candidate_revision" \
  --arg runtimeBaseDigest "$runtime_base_digest" \
  --arg semanticResponseSha256 "$semantic_sha" \
  --arg candidateMetadataSha256 "$candidate_metadata_sha" \
  --arg staticClosureSha256 "$static_closure_sha" \
  --arg nativeSbomSha256 "$native_sbom_sha" \
  --arg spdxSbomSha256 "$spdx_sbom_sha" \
  --arg vulnerabilityReportSha256 "$scan_report_sha" \
  --arg databaseSchema "$database_schema" \
  --arg databaseBuilt "$database_built" \
  --arg databaseSource "$database_source" \
  --argjson staticClosureEntries "$static_closure_entries" \
  --argjson syftDiscoveredPackages "$package_count" \
  --argjson vulnerabilityMatches "$match_count" \
  --argjson grypeExitStatus "$scan_status" \
  --argjson databaseValid "$database_valid" \
  '{schemaVersion: 1, admission: "UNADMITTED", architecture: $architecture,
    platformRevision: $platformRevision, imageId: $imageId,
    syftImageId: $syftImageId, manifestDigest: $manifestDigest,
    runtimeBaseDigest: $runtimeBaseDigest,
    semanticResponseSha256: $semanticResponseSha256,
    candidateMetadataSha256: $candidateMetadataSha256,
    tools: {syft: "1.50.0", grype: "0.116.1"},
    sbom: {nativeSha256: $nativeSbomSha256, spdxSha256: $spdxSbomSha256,
      syftDiscoveredPackages: $syftDiscoveredPackages,
      staticClosureSha256: $staticClosureSha256,
      staticClosureEntries: $staticClosureEntries,
      coverage: [
        "Syft image/package/file catalog for the final OCI rootfs",
        "compiler-owned static native and Rust closure with immutable license evidence"
      ],
      limitation: "Syft/Grype may not infer every component linked into a stripped static ELF; the compiler-owned closure remains an independent required evidence stream"},
    vulnerability: {reportSha256: $vulnerabilityReportSha256,
      matches: $vulnerabilityMatches, failThreshold: "high",
      grypeExitStatus: $grypeExitStatus,
      database: {schema: $databaseSchema, built: $databaseBuilt, source: $databaseSource,
        valid: $databaseValid}}}' \
  > "$scan_metadata"
chmod 0444 "$native_sbom" "$spdx_sbom" "$scan_report" "$scan_metadata"

printf 'security_gate=%s\n' "$([[ "$scan_status" -eq 0 ]] && printf pass || printf fail)"
printf 'architecture=%s\n' "$architecture"
printf 'manifest_digest=%s\n' "$manifest_digest"
printf 'syft_discovered_packages=%s\n' "$package_count"
printf 'static_closure_entries=%s\n' "$static_closure_entries"
printf 'vulnerability_matches=%s\n' "$match_count"
printf 'evidence_directory=%s\n' "$output_dir"

[[ "$scan_status" -eq 0 ]] || fail 'Grype found a vulnerability at or above the high-severity threshold'

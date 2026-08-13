#!/usr/bin/env bash
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
script="$root/scripts/assemble-optimizer-candidate-gate.sh"
fixture="$(mktemp -d /tmp/platform-optimizer-candidate-gate-test.XXXXXXXX)"
test_repository="$fixture/repository"

cleanup() {
  case "$fixture" in
    /tmp/platform-optimizer-candidate-gate-test.*) rm -rf -- "$fixture" ;;
  esac
}
trap cleanup EXIT

mkdir -p "$test_repository"
git -C "$test_repository" init -q -b develop
git -C "$test_repository" config user.name 'Optimizer Fixture'
git -C "$test_repository" config user.email 'optimizer-fixture@example.invalid'
printf 'immutable fixture\n' > "$test_repository/source.txt"
git -C "$test_repository" add source.txt
git -C "$test_repository" commit -q -m fixture
revision="$(git -C "$test_repository" rev-parse HEAD)"

run_gate() {
  (cd "$test_repository" && "$script" "$@")
}

write_architecture() {
  local backend="$1" architecture="$2" root_dir="$3" semantic="$4" request="$5"
  local candidate="$root_dir/${backend}-${architecture}"
  local security="$root_dir/${backend}-${architecture}-security"
  mkdir -p "$candidate" "$security"
  printf '%s\n' "$semantic" > "$candidate/semantic-response.json"
  printf 'image-%s\n' "$architecture" | gzip -1 -n > "$root_dir/${backend}-${architecture}-image.tar.gz"
  semantic_sha="$(sha256sum "$candidate/semantic-response.json" | cut -d' ' -f1)"
  jq -n \
    --arg architecture "$architecture" \
    --arg revision "$revision" \
    --arg semantic "$semantic_sha" \
    --arg request "$request" \
    '{schemaVersion: 1, admission: "UNADMITTED", architecture: $architecture,
      platformRevision: $revision, imageId: ("sha256:" + ("1" * 64)),
      binarySha256: ("2" * 64), semanticResponseSha256: $semantic,
      requestSha256: (if $request == "" then null else $request end)} |
      with_entries(select(.value != null))' > "$candidate/candidate-metadata.json"
  printf '{"spdxVersion":"SPDX-2.3"}\n' > "$security/${backend}-${architecture}.spdx.json"
  printf '{"descriptor":{"name":"grype"}}\n' > "$security/${backend}-${architecture}.grype.json"
  candidate_sha="$(sha256sum "$candidate/candidate-metadata.json" | cut -d' ' -f1)"
  spdx_sha="$(sha256sum "$security/${backend}-${architecture}.spdx.json" | cut -d' ' -f1)"
  scan_sha="$(sha256sum "$security/${backend}-${architecture}.grype.json" | cut -d' ' -f1)"
  jq -n \
    --arg architecture "$architecture" \
    --arg revision "$revision" \
    --arg candidate "$candidate_sha" \
    --arg spdx "$spdx_sha" \
    --arg scan "$scan_sha" \
    '{schemaVersion: 1, admission: "UNADMITTED", architecture: $architecture,
      platformRevision: $revision, candidateMetadataSha256: $candidate,
      tools: {syft: "1.50.0", grype: "0.116.1"},
      sbom: {spdxSha256: $spdx},
      vulnerability: {reportSha256: $scan, grypeExitStatus: 0,
        database: {valid: true, source: "fixture-db"}}}' \
    > "$security/security-metadata.json"
  jq -n \
    --arg backend "$backend" \
    --arg architecture "$architecture" \
    --arg revision "$revision" \
    '{apiVersion: "optimization.platform.example/optimizer-reproducibility/v1alpha1",
      kind: "OptimizerCandidateReproducibility", admission: "UNADMITTED",
      backend: $backend, architecture: $architecture, platformRevision: $revision,
      cachePruned: true, noCache: true,
      firstImageId: ("sha256:" + ("1" * 64)),
      secondImageId: ("sha256:" + ("1" * 64)),
      byteIdenticalImage: true, registryMutated: false,
      optimizerStateMutated: false}' \
    > "$root_dir/${backend}-${architecture}-reproducibility.json"
}

expect_failure() {
  local name="$1"
  shift
  if "$@" >"$fixture/${name}.out" 2>&1; then
    printf 'expected failure: %s\n' "$name" >&2
    exit 1
  fi
}

request="$(printf 'request' | sha256sum | cut -d' ' -f1)"
mkdir -p "$fixture/highs-amd64" "$fixture/highs-arm64"
write_architecture highs amd64 "$fixture/highs-amd64" '{"termination":"OPTIMAL"}' "$request"
write_architecture highs arm64 "$fixture/highs-arm64" '{"termination":"OPTIMAL"}' "$request"
run_gate highs "$revision" "$fixture/highs-amd64" "$fixture/highs-arm64" "$fixture/highs.json"
jq -e --arg revision "$revision" --arg request "$request" '
  .apiVersion == "optimization.platform.example/optimizer-candidate-gate/v1alpha1" and
  .kind == "OptimizerCandidateGate" and .admission == "UNADMITTED" and
  .selectable == false and .backend == "highs" and .platformRevision == $revision and
  .requestSha256 == $request and .gates.nativeMultiArchitecture == true and
  .gates.exactSemanticEquivalence == true and .gates.cacheIndependentRebuild == true and
  .registryMutated == false and
  .optimizerStateMutated == false and
  (.sourceTreeDigest | test("^sha256:[0-9a-f]{64}$"))
' "$fixture/highs.json" >/dev/null

mkdir -p "$fixture/cpsat-amd64" "$fixture/cpsat-arm64"
write_architecture cpsat amd64 "$fixture/cpsat-amd64" '{"termination":"OPTIMAL"}' ''
write_architecture cpsat arm64 "$fixture/cpsat-arm64" '{"termination":"OPTIMAL"}' ''
run_gate cpsat "$revision" "$fixture/cpsat-amd64" "$fixture/cpsat-arm64" "$fixture/cpsat.json"
jq -e '.backend == "cpsat" and .requestSha256 == null' "$fixture/cpsat.json" >/dev/null

printf '{"termination":"FEASIBLE"}\n' > "$fixture/cpsat-arm64/cpsat-arm64/semantic-response.json"
expect_failure semantic_mismatch run_gate cpsat "$revision" \
  "$fixture/cpsat-amd64" "$fixture/cpsat-arm64" "$fixture/semantic-mismatch.json"

cp "$fixture/highs-amd64/highs-amd64-security/security-metadata.json" "$fixture/tampered-security.json"
jq '.admission = "ENABLED"' "$fixture/tampered-security.json" > \
  "$fixture/highs-amd64/highs-amd64-security/security-metadata.json"
expect_failure relabelled_admission run_gate highs "$revision" \
  "$fixture/highs-amd64" "$fixture/highs-arm64" "$fixture/relabelled.json"
mv "$fixture/tampered-security.json" "$fixture/highs-amd64/highs-amd64-security/security-metadata.json"

expect_failure abbreviated_revision run_gate highs "${revision:0:12}" \
  "$fixture/highs-amd64" "$fixture/highs-arm64" "$fixture/abbreviated.json"

ln -s "$fixture/highs-amd64/highs-amd64/candidate-metadata.json" "$fixture/symbolic-metadata.json"
rm "$fixture/highs-amd64/highs-amd64/candidate-metadata.json"
mv "$fixture/symbolic-metadata.json" "$fixture/highs-amd64/highs-amd64/candidate-metadata.json"
expect_failure symbolic_evidence run_gate highs "$revision" \
  "$fixture/highs-amd64" "$fixture/highs-arm64" "$fixture/symbolic.json"

printf 'dirty\n' >> "$test_repository/source.txt"
expect_failure dirty_checkout run_gate highs "$revision" \
  "$fixture/highs-amd64" "$fixture/highs-arm64" "$fixture/dirty.json"

git -C "$test_repository" checkout -q -- source.txt
rm -f "$fixture/highs-amd64/highs-amd64/candidate-metadata.json"
write_architecture highs amd64 "$fixture/highs-amd64" '{"termination":"OPTIMAL"}' "$request"
jq '.cachePruned = false' \
  "$fixture/highs-arm64/highs-arm64-reproducibility.json" > "$fixture/foreign-reproducibility.json"
mv "$fixture/foreign-reproducibility.json" \
  "$fixture/highs-arm64/highs-arm64-reproducibility.json"
expect_failure cached_rebuild run_gate highs "$revision" \
  "$fixture/highs-amd64" "$fixture/highs-arm64" "$fixture/cached-rebuild.json"

printf 'optimizer candidate gate fixtures: pass\n'

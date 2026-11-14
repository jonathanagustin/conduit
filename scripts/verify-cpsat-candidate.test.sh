#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'verify-cpsat-candidate.test: %s\n' "$*" >&2
  exit 1
}

script_dir="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
platform_dir="$(CDPATH= cd -- "$script_dir/.." && pwd)"
verifier="$script_dir/verify-cpsat-candidate.sh"
scanner="$script_dir/scan-cpsat-candidate.sh"
containerfile="$platform_dir/Containerfile.optimizer-cpsat"
evidence="$platform_dir/docs/evidence/or-tools-9.15-cpsat-rust-1.99.1-amd64.json"
# Repository root, not one above it. The `../.github` this carried is left over
# from the monorepo subdirectory this code was extracted from; here it resolves
# outside the checkout, so the hash below failed and took the candidate
# workflow's contract job with it.
workflow="$platform_dir/.github/workflows/platform-cpsat-candidate.yml"

printf '%s  %s\n' '8f36578298d697519779b2fd4f867647333dca1c38e5788a74a4aaa7e379e9bf' "$platform_dir/docs/evidence/or-tools-9.15-cpsat-candidate-amd64.json" | sha256sum --check --status || fail 'historical evidence changed'

bash -n "$verifier"
bash -n "$scanner"
bash -n "${BASH_SOURCE[0]}"

jq -e \
  --arg containerfile "$(sha256sum "$containerfile" | cut -d' ' -f1)" \
  --arg verifier "$(sha256sum "$verifier" | cut -d' ' -f1)" \
  --arg fixture "$(sha256sum "${BASH_SOURCE[0]}" | cut -d' ' -f1)" \
  --arg scanner "$(sha256sum "$scanner" | cut -d' ' -f1)" \
  --arg workflow "$(sha256sum "$workflow" | cut -d' ' -f1)" '
    .schemaVersion == 1 and .admission == "UNADMITTED" and .projectLicenseStatus == "NOASSERTION" and .architecture == "amd64" and
    .source.containerfileSha256 == $containerfile and .source.verifierSha256 == $verifier and
    .source.verifierFixtureSha256 == $fixture and .source.scannerSha256 == $scanner and
    .source.workflowSha256 == $workflow and
    .candidate.kind == "LOCAL_UNADMITTED_OCI" and .candidate.semanticNativeRuns == 5 and
    .candidate.runtimeUser == "65532:65532" and .candidate.dynamicNeededEntries == 0 and
    .candidate.staticClosureEntries == 10 and .candidate.binarySizeBytes == 37713792 and
    .candidate.binarySha256 == "b2e24f21e136c5f652ba5a9c1589b4a743866ce515822a08a0d980a51b0038ed" and
    .candidate.semanticResponseSha256 == "5ffbb0e0e427b4d917dafd072d0b0c001ef49c5150377a20957d8704e46f1065" and
    (.candidate.imageId | test("^sha256:[0-9a-f]{64}$")) and
    .toolchain.syft == "1.50.0" and .toolchain.grype == "0.116.1" and
    .security.syftDiscoveredPackages == 5 and
    .security.staticClosureEntries == 10 and .security.vulnerabilityMatches == 0 and
    .security.failThreshold == "high" and .security.databaseValid == true and
    (.security.metadataSha256 | test("^[0-9a-f]{64}$")) and
    (.security.syftJsonSha256 | test("^[0-9a-f]{64}$")) and
    (.security.spdxJsonSha256 | test("^[0-9a-f]{64}$")) and
    (.security.grypeJsonSha256 | test("^[0-9a-f]{64}$")) and
    (.verifiedGates | length) == 11 and (.openAdmissionGates | length) == 6
  ' "$evidence" >/dev/null || fail 'committed amd64 candidate evidence is stale or malformed'

for required in \
  'actions/setup-go@924ae3a1cded613372ab5595356fb5720e22ba16' \
  'actions/cache@caa296126883cff596d87d8935842f9db880ef25' \
  '--build-arg "SOURCE_DATE_EPOCH=1768232310"' \
  'ubuntu-24.04-arm' \
  'syft_1.50.0_linux_${ARCHITECTURE}.tar.gz' \
  'grype_0.116.1_linux_${ARCHITECTURE}.tar.gz' \
  '887c57cbcc2d0e8c5c110a4571a3fc7150058b24d74f993ee4663516e5c8ce86' \
  'a8d7504a149629324eb5f4ce3dc25dfd211bbfe047e64ee2bf7844b466c3d84d' \
  './scripts/scan-cpsat-candidate.sh' \
  'cmp "${amd64_dir}/semantic-response.json" "${arm64_dir}/semantic-response.json"'; do
  grep -Fq -- "$required" "$workflow" || fail "candidate workflow omits supply-chain contract: $required"
done

tmp_dir="$(mktemp -d -t verify-cpsat-candidate-test.XXXXXXXX)"
cleanup() {
  if [[ -d "$tmp_dir" && "$tmp_dir" == /tmp/verify-cpsat-candidate-test.* ]]; then
    rm -rf -- "$tmp_dir"
  fi
}
trap cleanup EXIT

"$verifier" --self-audit
"$verifier" --containerfile "$containerfile"

cp "$containerfile" "$tmp_dir/Containerfile"
sed -i 's/io.palantir.platform.optimizer.admission="UNADMITTED"/io.palantir.platform.optimizer.admission="ADMITTED"/' "$tmp_dir/Containerfile"
if "$verifier" --containerfile "$tmp_dir/Containerfile" >/dev/null 2>&1; then
  printf 'mutated admission label passed verification\n' >&2
  exit 1
fi

cp "$containerfile" "$tmp_dir/Containerfile"
sed -i 's/USER 65532:65532/USER 0:0/' "$tmp_dir/Containerfile"
if "$verifier" --containerfile "$tmp_dir/Containerfile" >/dev/null 2>&1; then
  printf 'mutated runtime user passed verification\n' >&2
  exit 1
fi

cp "$containerfile" "$tmp_dir/Containerfile"
sed -i 's/USE_BAZEL_VERSION=8.7.0/USE_BAZEL_VERSION=latest/' "$tmp_dir/Containerfile"
if "$verifier" --containerfile "$tmp_dir/Containerfile" >/dev/null 2>&1; then
  printf 'unversioned Bazel passed verification\n' >&2
  exit 1
fi

cp "$containerfile" "$tmp_dir/Containerfile"
sed -i '/strip --strip-all \/out\/platform-cpsat-adapter/d' "$tmp_dir/Containerfile"
if "$verifier" --containerfile "$tmp_dir/Containerfile" >/dev/null 2>&1; then
  printf 'unstripped candidate recipe passed verification\n' >&2
  exit 1
fi

cp "$containerfile" "$tmp_dir/Containerfile"
sed -i "s/grep -Fq 'statically linked'/grep -Fq 'dynamically linked'/" "$tmp_dir/Containerfile"
if "$verifier" --containerfile "$tmp_dir/Containerfile" >/dev/null 2>&1; then
  printf 'dynamic candidate recipe passed verification\n' >&2
  exit 1
fi

cp "$containerfile" "$tmp_dir/Containerfile"
sed -i 's/FROM ${RUNTIME_IMAGE}/FROM busybox:latest/' "$tmp_dir/Containerfile"
if "$verifier" --containerfile "$tmp_dir/Containerfile" >/dev/null 2>&1; then
  printf 'mutable unpinned runtime passed verification\n' >&2
  exit 1
fi

cp "$containerfile" "$tmp_dir/Containerfile"
sed -i '/platform-source-hashes\.sha256/d' "$tmp_dir/Containerfile"
if "$verifier" --containerfile "$tmp_dir/Containerfile" >/dev/null 2>&1; then
  printf 'candidate without Platform source binding passed verification\n' >&2
  exit 1
fi

cp "$containerfile" "$tmp_dir/Containerfile"
sed -i '/link-action\.textproto/d' "$tmp_dir/Containerfile"
if "$verifier" --containerfile "$tmp_dir/Containerfile" >/dev/null 2>&1; then
  printf 'candidate without link-action evidence passed verification\n' >&2
  exit 1
fi

if "$verifier" example.invalid/candidate:missing "$tmp_dir" >/dev/null 2>&1; then
  printf 'verifier accepted a pre-existing evidence directory\n' >&2
  exit 1
fi

mkdir "$tmp_dir/minimal-candidate"
printf '%s\n' 'component\tversion\tlicense-evidence' 'Platform-adapter\tWORKTREE\tlicenses/PLATFORM-LICENSE' \
  > "$tmp_dir/minimal-candidate/static-closure.tsv"
static_closure_sha="$(sha256sum "$tmp_dir/minimal-candidate/static-closure.tsv" | cut -d' ' -f1)"
jq --null-input \
  --arg staticClosureSha256 "$static_closure_sha" \
  '{architecture:"amd64", imageId:("sha256:" + ("a" * 64)), platformRevision:"WORKTREE",
    staticClosureSha256:$staticClosureSha256, staticClosureEntries:1,
    semanticResponseSha256:("b" * 64), runtimeBaseDigest:("sha256:" + ("c" * 64))}' \
  > "$tmp_dir/minimal-candidate/candidate-metadata.json"
printf '%s\n' '#!/usr/bin/env bash' \
  'printf '\''{"application":"syft","version":"0.0.0"}\\n'\''' > "$tmp_dir/wrong-syft"
printf '%s\n' '#!/usr/bin/env bash' \
  'printf '\''{"application":"grype","version":"0.116.1"}\\n'\''' > "$tmp_dir/right-grype"
chmod 0755 "$tmp_dir/wrong-syft" "$tmp_dir/right-grype"
if SYFT_BIN="$tmp_dir/wrong-syft" GRYPE_BIN="$tmp_dir/right-grype" \
  "$scanner" unavailable "$tmp_dir/minimal-candidate" "$tmp_dir/wrong-syft-output" \
  >/dev/null 2>&1; then
  fail 'scanner accepted an unpinned Syft version'
fi

jq '.staticClosureSha256 = "not-a-digest"' "$tmp_dir/minimal-candidate/candidate-metadata.json" \
  > "$tmp_dir/minimal-candidate/invalid-candidate-metadata.json"
mv "$tmp_dir/minimal-candidate/invalid-candidate-metadata.json" \
  "$tmp_dir/minimal-candidate/candidate-metadata.json"
printf '%s\n' '#!/usr/bin/env bash' \
  'printf '\''{"application":"syft","version":"1.50.0"}\\n'\''' > "$tmp_dir/right-syft"
chmod 0755 "$tmp_dir/right-syft"
if SYFT_BIN="$tmp_dir/right-syft" GRYPE_BIN="$tmp_dir/right-grype" \
  "$scanner" unavailable "$tmp_dir/minimal-candidate" "$tmp_dir/invalid-closure-output" \
  >/dev/null 2>&1; then
  fail 'scanner accepted an invalid static-closure digest'
fi

printf 'verify-cpsat-candidate fixtures: PASS\n'

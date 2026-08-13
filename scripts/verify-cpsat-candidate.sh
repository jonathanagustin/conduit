#!/usr/bin/env bash
set -euo pipefail

EXPECTED_VERSION='9.15.6755-unadmitted.1'
EXPECTED_SOURCE_SHA='599c870319bb127441d92c452d8f79bca46ca6fd295c1deb8031ed303a361311'
EXPECTED_UPSTREAM_COMMIT='551ad10d94835c99e5e1e684500d3db398c0e345'
EXPECTED_BUILDER_DIGEST='8fa55b2f3ddf97471ab6a767bfa3f37e6bad0986ba823e75fea57e2a2a5c3073'
EXPECTED_RUNTIME_DIGEST='1b7b9f0f0e0a1d2155f531db587cc48ec26aaf97ab64364225f5bf18a054e66a'
EXPECTED_BAZELISK_AMD64_SHA='5a408715e932c0250d28bd84555f12edbf70117de42f9181691c736eacc4a992'
EXPECTED_BAZELISK_ARM64_SHA='e20e8b0f4f240091b7a55bf17b9398bd4f40ee70ae0208dff95dd4c445fb4010'
MAXIMUM_BINARY_BYTES=67108864
NATIVE_RUNS=5
EXPECTED_PLATFORM_REVISION="${EXPECTED_PLATFORM_REVISION:-}"

fail() {
  printf 'verify-cpsat-candidate: %s\n' "$*" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "required command is unavailable: $1"
}

require_text() {
  local path="$1"
  local expected="$2"
  grep -Fq -- "$expected" "$path" || fail "$path lacks required pin or gate: $expected"
}

script_dir="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
platform_dir="$(CDPATH= cd -- "$script_dir/.." && pwd)"
containerfile="$platform_dir/Containerfile.optimizer-cpsat"

verify_containerfile() {
  local path="$1"
  local final_from
  [[ -f "$path" ]] || fail "Containerfile does not exist: $path"
  require_text "$path" "rust:1.97.0-bookworm@sha256:${EXPECTED_BUILDER_DIGEST}"
  require_text "$path" "gcr.io/distroless/static-debian12:nonroot@sha256:${EXPECTED_RUNTIME_DIGEST}"
  require_text "$path" "ADD --checksum=sha256:${EXPECTED_SOURCE_SHA}"
  require_text "$path" "bazelisk_sha='${EXPECTED_BAZELISK_AMD64_SHA}'"
  require_text "$path" "bazelisk_sha='${EXPECTED_BAZELISK_ARM64_SHA}'"
  require_text "$path" 'test "$BUILDARCH" = "$TARGETARCH"'
  require_text "$path" "test \"\$(rustc --version)\" = 'rustc 1.97.0 (2d8144b78 2026-07-07)'"
  require_text "$path" 'USE_BAZEL_VERSION=8.7.0'
  require_text "$path" 'id=platform-cpsat-cargo,target=/tmp/platform-cpsat-cargo,sharing=locked'
  require_text "$path" 'id=platform-cpsat-rust-target,target=/tmp/platform-cpsat-target,sharing=locked'
  require_text "$path" 'target=/tmp/platform-cpsat-bazel,sharing=locked'
  require_text "$path" 'target=/root/.cache/bazelisk,sharing=locked'
  require_text "$path" '--jobs=2'
  require_text "$path" 'cargo test --manifest-path /src/adapter/Cargo.toml --locked'
  require_text "$path" 'cargo build --manifest-path /src/adapter/Cargo.toml --locked --release --lib --features native'
  require_text "$path" "cargo tree --manifest-path /src/adapter/Cargo.toml --locked --edges normal"
  require_text "$path" '//ortools/sat/c_api:platform-cpsat-adapter'
  require_text "$path" 'strip --strip-all /out/platform-cpsat-adapter'
  require_text "$path" 'platform-source-hashes.sha256'
  require_text "$path" 'license-hashes.sha256'
  require_text "$path" 'static-closure.tsv'
  require_text "$path" 'link-action.textproto'
  require_text "$path" "grep -Fq 'statically linked'"
  require_text "$path" "io.palantir.platform.optimizer.upstream.commit=\"${EXPECTED_UPSTREAM_COMMIT}\""
  require_text "$path" 'io.palantir.platform.optimizer.admission="UNADMITTED"'
  require_text "$path" 'USER 65532:65532'
  require_text "$path" 'ENTRYPOINT ["/platform-cpsat-adapter"]'
  require_text "$path" "if grep -Eq 'NEEDED|libstdc\\+\\+|libgcc_s'"
  final_from="$(awk 'toupper($1) == "FROM" { line=$0 } END { print line }' "$path")"
  [[ "$final_from" == 'FROM ${RUNTIME_IMAGE}' ]] || fail "candidate final stage is not the pinned static runtime: $final_from"
}

verify_self_audit() {
  local path="$1"
  [[ -f "$path" ]] || fail "verifier does not exist: $path"
  require_text "$path" 'MAXIMUM_BINARY_BYTES=67108864'
  require_text "$path" 'NATIVE_RUNS=5'
  require_text "$path" 'docker image inspect'
  require_text "$path" "grep -Eq 'NEEDED|libstdc\\+\\+|libgcc_s'"
  require_text "$path" "grep -Fq 'statically linked'"
  require_text "$path" 'platform-source-hashes.sha256'
  require_text "$path" 'license-hashes.sha256'
  require_text "$path" 'static-closure.tsv'
  require_text "$path" 'link-action.textproto'
  require_text "$path" 'TestExternalProcessDiscreteBackendRealCpSat'
  require_text "$path" '--network none'
  require_text "$path" '--read-only'
  require_text "$path" '--cap-drop ALL'
  require_text "$path" '--security-opt no-new-privileges:true'
  require_text "$path" '--pids-limit 128'
  require_text "$path" '--memory 512m'
  require_text "$path" '--cpus 1'
  require_text "$path" 'PLATFORM_CPSAT_VERIFY_GOMODCACHE:-/tmp/platform-cpsat-verify/go-modcache'
  require_text "$path" 'PLATFORM_CPSAT_VERIFY_GOCACHE:-/tmp/platform-cpsat-verify/go-cache'
  require_text "$path" 'PLATFORM_CPSAT_SEMANTIC_EVIDENCE'
  require_text "$path" 'candidate-metadata.json'
  require_text "$path" 'semantic-response.json'
}

case "${1:-}" in
  --containerfile)
    [[ $# -eq 2 ]] || fail 'usage: verify-cpsat-candidate.sh --containerfile PATH'
    verify_containerfile "$2"
    printf 'containerfile_gate=pass\n'
    exit 0
    ;;
  --self-audit)
    [[ $# -le 2 ]] || fail 'usage: verify-cpsat-candidate.sh --self-audit [PATH]'
    verify_self_audit "${2:-${BASH_SOURCE[0]}}"
    printf 'verifier_self_audit=pass\n'
    exit 0
    ;;
esac

[[ $# -ge 1 && $# -le 2 ]] || fail 'usage: verify-cpsat-candidate.sh IMAGE [NEW_EVIDENCE_DIRECTORY]'
image="$1"
for command_name in docker jq file readelf sha256sum stat awk grep mktemp tar go uname install find; do
  require_command "$command_name"
done
verify_containerfile "$containerfile"
verify_self_audit "${BASH_SOURCE[0]}"

case "$(uname -m)" in
  x86_64) native_arch='amd64'; expected_machine='Advanced Micro Devices X86-64' ;;
  aarch64|arm64) native_arch='arm64'; expected_machine='AArch64' ;;
  *) fail "unsupported native host architecture: $(uname -m)" ;;
esac

tmp_dir="$(mktemp -d -t verify-cpsat-candidate.XXXXXXXX)"
go_mod_cache="${PLATFORM_CPSAT_VERIFY_GOMODCACHE:-/tmp/platform-cpsat-verify/go-modcache}"
go_build_cache="${PLATFORM_CPSAT_VERIFY_GOCACHE:-/tmp/platform-cpsat-verify/go-cache}"
case "$go_mod_cache:$go_build_cache" in
  /tmp/*:/tmp/*) ;;
  *) fail 'CP-SAT verifier Go caches must be explicit paths below /tmp' ;;
esac
mkdir -p "$go_mod_cache" "$go_build_cache"
container_id=''
cleanup() {
  if [[ -n "$container_id" ]]; then
    docker rm -f "$container_id" >/dev/null 2>&1 || true
  fi
  if [[ -d "$tmp_dir" && "$tmp_dir" == /tmp/verify-cpsat-candidate.* ]]; then
    chmod -R u+w "$tmp_dir" 2>/dev/null || true
    rm -rf -- "$tmp_dir"
  fi
}
trap cleanup EXIT

results_dir="$tmp_dir/results"
if [[ $# -eq 2 ]]; then
  [[ ! -e "$2" ]] || fail "evidence directory already exists: $2"
  mkdir -m 0755 -p -- "$2"
  results_dir="$2"
else
  mkdir -m 0755 "$results_dir"
fi

docker image inspect "$image" > "$tmp_dir/inspect.json"
runtime_image="gcr.io/distroless/static-debian12:nonroot@sha256:${EXPECTED_RUNTIME_DIGEST}"
docker image inspect "$runtime_image" > "$tmp_dir/runtime-inspect.json" \
  || fail 'pinned distroless static runtime is unavailable locally'
jq -e \
  --arg arch "$native_arch" \
  --arg version "$EXPECTED_VERSION" \
  --arg revision "$EXPECTED_PLATFORM_REVISION" \
  --arg source "$EXPECTED_SOURCE_SHA" \
  --arg commit "$EXPECTED_UPSTREAM_COMMIT" '
    .[0].Architecture == $arch and
    .[0].Os == "linux" and
    .[0].Config.User == "65532:65532" and
    .[0].Config.Entrypoint == ["/platform-cpsat-adapter"] and
    .[0].Config.Labels["org.opencontainers.image.version"] == $version and
    ($revision == "" or .[0].Config.Labels["org.opencontainers.image.revision"] == $revision) and
    .[0].Config.Labels["io.palantir.platform.optimizer.admission"] == "UNADMITTED" and
    .[0].Config.Labels["io.palantir.platform.optimizer.architecture"] == $arch and
    .[0].Config.Labels["io.palantir.platform.optimizer.upstream.source.sha256"] == $source and
    .[0].Config.Labels["io.palantir.platform.optimizer.upstream.commit"] == $commit and
    .[0].Config.Labels["io.palantir.platform.optimizer.runtime.base.digest"] == ("sha256:" + "1b7b9f0f0e0a1d2155f531db587cc48ec26aaf97ab64364225f5bf18a054e66a") and
    .[0].Config.Labels["org.opencontainers.image.licenses"] == "NOASSERTION" and
    .[0].Config.Labels["io.palantir.platform.optimizer.rust.version"] == "1.97.0" and
    .[0].Config.Labels["io.palantir.platform.optimizer.bazel.version"] == "8.7.0"
  ' "$tmp_dir/inspect.json" >/dev/null || fail 'candidate image config or labels differ from the contract'
jq -e -s '
    .[0][0].RootFS.Layers as $candidate |
    .[1][0].RootFS.Layers as $runtime |
    ($candidate | length) == (($runtime | length) + 1) and
    $candidate[0:($runtime | length)] == $runtime
  ' "$tmp_dir/inspect.json" "$tmp_dir/runtime-inspect.json" >/dev/null \
  || fail 'candidate rootfs is not exactly the pinned static runtime plus one candidate layer'

container_id="$(docker create "$image")"
docker export --output "$tmp_dir/rootfs.tar" "$container_id"
mkdir "$tmp_dir/rootfs"
tar --extract --file "$tmp_dir/rootfs.tar" --directory "$tmp_dir/rootfs"
binary="$tmp_dir/rootfs/platform-cpsat-adapter"
evidence_root="$tmp_dir/rootfs/usr/share/platform-cpsat/evidence"
license_root="$tmp_dir/rootfs/usr/share/platform-cpsat/licenses"
[[ -x "$binary" ]] || fail 'candidate executable is absent or not executable'
binary_size="$(stat -c '%s' "$binary")"
[[ "$binary_size" =~ ^[0-9]+$ ]] || fail 'candidate size is not numeric'
(( binary_size <= MAXIMUM_BINARY_BYTES )) || fail 'candidate exceeds the 64 MiB executable bound'
file "$binary" | grep -Fq 'ELF 64-bit' || fail 'candidate is not a 64-bit ELF executable'
file "$binary" | grep -Fq 'statically linked' || fail 'candidate is not fully statically linked'
readelf -h "$binary" | grep -Fq "Machine:                           $expected_machine" || fail 'candidate ELF architecture is foreign'
if readelf -d "$binary" | grep -Eq 'NEEDED|libstdc\+\+|libgcc_s'; then
  fail 'candidate unexpectedly carries a dynamic runtime dependency'
fi
(
  cd "$tmp_dir/rootfs"
  sha256sum --check usr/share/platform-cpsat/evidence/platform-cpsat-adapter.sha256 >/dev/null
) || fail 'candidate executable does not match embedded build evidence'
for evidence_name in \
  build-inputs.txt cargo-closure.tsv dynamic-section.txt file.txt \
  license-hashes.sha256 link-action.textproto platform-cpsat-adapter.sha256 \
  platform-source-hashes.sha256 static-closure.tsv; do
  [[ -s "$evidence_root/$evidence_name" ]] || fail "candidate lacks nonempty evidence: $evidence_name"
  [[ "$(stat -c '%a' "$evidence_root/$evidence_name")" == 444 ]] \
    || fail "candidate evidence is not immutable: $evidence_name"
done
[[ -d "$license_root" ]] || fail 'candidate lacks its static license closure'
(
  cd "$tmp_dir/rootfs/usr/share/platform-cpsat"
  sha256sum --check evidence/license-hashes.sha256 >/dev/null
) || fail 'candidate license files do not match their embedded digest manifest'
(
  cd "$platform_dir"
  sha256sum --check "$evidence_root/platform-source-hashes.sha256" >/dev/null
) || fail 'candidate does not bind to the checked-out Platform adapter and verifier inputs'
for closure_component in OR-Tools Abseil Protobuf zlib Rust glibc GCC-runtime Cargo-normal-closure Distroless-static-debian12 Platform-adapter; do
  grep -Fq "$closure_component" "$evidence_root/static-closure.tsv" \
    || fail "candidate static closure omits $closure_component"
done
grep -Fq 'CppLink' "$evidence_root/link-action.textproto" \
  || fail 'candidate link-action evidence omits the Bazel C++ link action'
grep -Fq 'libplatform_cpsat_adapter.a' "$evidence_root/link-action.textproto" \
  || fail 'candidate link-action evidence omits the Rust adapter archive'
binary_mode="$(stat -c '%a' "$binary")"
(( (8#$binary_mode & 0222) == 0 && (8#$binary_mode & 0100) != 0 )) \
  || fail 'candidate executable is writable or not owner-executable'
if find "$binary" "$tmp_dir/rootfs/usr/share/platform-cpsat" -perm /022 -print -quit | grep -q .; then
  fail 'candidate-owned rootfs contains a group- or world-writable entry'
fi
if find "$tmp_dir/rootfs/usr/share/platform-cpsat" \( -type l -o -type p -o -type s -o -type b -o -type c \) -print -quit | grep -q .; then
  fail 'candidate-owned rootfs contains an unexpected non-file entry'
fi
root_files="$((1 + $(find "$tmp_dir/rootfs/usr/share/platform-cpsat" -type f | wc -l)))"
(( root_files <= 512 )) || fail 'candidate scratch rootfs exceeds the 512-file bound'
[[ ! -e "$tmp_dir/rootfs/bin/sh" && ! -e "$tmp_dir/rootfs/busybox/sh" ]] || fail 'candidate runtime unexpectedly contains a shell'

set +e
invalid_output="$(printf '{}\n' | docker run --rm -i \
  --network none --read-only --cap-drop ALL --security-opt no-new-privileges:true \
  --pids-limit 128 --memory 512m --cpus 1 "$image" 2>&1)"
invalid_rc=$?
set -e
if (( invalid_rc == 0 )); then
  fail 'candidate accepted an invalid request in the isolated runtime'
fi
(( invalid_rc != 125 && invalid_rc != 126 && invalid_rc != 127 )) \
  || fail "candidate could not execute in the isolated runtime: $invalid_output"
grep -Eqi 'invalid|error|request' <<<"$invalid_output" \
  || fail 'candidate rejected an invalid request without a diagnostic'

(
  cd "$platform_dir"
  PLATFORM_CPSAT_ADAPTER="$binary" \
    PLATFORM_CPSAT_SEMANTIC_EVIDENCE="$tmp_dir/semantic-response.json" \
    GOMODCACHE="$go_mod_cache" \
    GOCACHE="$go_build_cache" \
    go test -race ./internal/optimization \
      -run TestExternalProcessDiscreteBackendRealCpSat \
      -count="$NATIVE_RUNS"
)

[[ -s "$tmp_dir/semantic-response.json" ]] || fail 'real CP-SAT semantic evidence was not produced'
jq -e '
  .schemaVersion == 1 and
  .requestId == "dsolreq_8b896187e5d555a363bb55d28142151a0cf7ae72759bf154818bee53c5b252e4" and
  .termination == "OPTIMAL" and
  .result.makespanSeconds == 8 and .result.estimatedTotalCostMicros == 24 and
  ([.result.assignments[].task] | sort) == ["prepare", "rollout-a", "rollout-b"] and
  .backendReportedEvidence.status == "OPTIMAL" and
  .backendReportedEvidence.proofKind == "CP_SAT_EXACT_LEXICOGRAPHIC_V1" and
  .backendReportedEvidence.trace.complete == true and
  .backendReportedEvidence.trace.stopReason == "COMPLETE" and
  (.backendReportedEvidence.trace.phases | length) == 5 and
  ([.backendReportedEvidence.trace.phases[].kind] | sort) ==
    ["CANONICAL_CHOICE", "CANONICAL_CHOICE", "CANONICAL_CHOICE", "OBJECTIVE", "OBJECTIVE"]
' "$tmp_dir/semantic-response.json" >/dev/null \
  || fail 'Platform semantic assertion rejected the real CP-SAT evidence'

image_id="$(jq -er '.[0].Id' "$tmp_dir/inspect.json")"
image_size="$(jq -er '.[0].Size' "$tmp_dir/inspect.json")"
platform_revision="$(jq -er '.[0].Config.Labels["org.opencontainers.image.revision"]' "$tmp_dir/inspect.json")"
binary_sha="$(sha256sum "$binary" | awk '{print $1}')"
semantic_sha="$(sha256sum "$tmp_dir/semantic-response.json" | awk '{print $1}')"
build_inputs_sha="$(sha256sum "$evidence_root/build-inputs.txt" | awk '{print $1}')"
static_closure_sha="$(sha256sum "$evidence_root/static-closure.tsv" | awk '{print $1}')"
static_closure_entries="$(awk 'NR > 1 { count++ } END { print count + 0 }' "$evidence_root/static-closure.tsv")"
cargo_closure_sha="$(sha256sum "$evidence_root/cargo-closure.tsv" | awk '{print $1}')"
license_hashes_sha="$(sha256sum "$evidence_root/license-hashes.sha256" | awk '{print $1}')"
link_action_sha="$(sha256sum "$evidence_root/link-action.textproto" | awk '{print $1}')"
source_hashes_sha="$(sha256sum "$evidence_root/platform-source-hashes.sha256" | awk '{print $1}')"
license_files="$(find "$license_root" -type f | wc -l)"

install -m 0444 "$tmp_dir/semantic-response.json" "$results_dir/semantic-response.json"
for evidence_name in \
  build-inputs.txt cargo-closure.tsv license-hashes.sha256 link-action.textproto \
  platform-source-hashes.sha256 static-closure.tsv; do
  install -m 0444 "$evidence_root/$evidence_name" "$results_dir/$evidence_name"
done
printf '%s  semantic-response.json\n' "$semantic_sha" > "$results_dir/semantic-response.sha256"
jq --null-input \
  --arg architecture "$native_arch" \
  --arg imageId "$image_id" \
  --arg binarySha256 "$binary_sha" \
  --arg semanticResponseSha256 "$semantic_sha" \
  --arg buildInputsSha256 "$build_inputs_sha" \
  --arg staticClosureSha256 "$static_closure_sha" \
  --arg cargoClosureSha256 "$cargo_closure_sha" \
  --arg licenseHashesSha256 "$license_hashes_sha" \
  --arg linkActionSha256 "$link_action_sha" \
  --arg platformSourceHashesSha256 "$source_hashes_sha" \
  --arg platformRevision "$platform_revision" \
  --arg runtimeBaseDigest "sha256:${EXPECTED_RUNTIME_DIGEST}" \
  --argjson imageSizeBytes "$image_size" \
  --argjson binarySizeBytes "$binary_size" \
  --argjson semanticRuns "$NATIVE_RUNS" \
  --argjson staticClosureEntries "$static_closure_entries" \
  --argjson licenseFiles "$license_files" \
  '{schemaVersion: 1, admission: "UNADMITTED", architecture: $architecture,
    platformRevision: $platformRevision, imageId: $imageId,
    imageSizeBytes: $imageSizeBytes, binarySizeBytes: $binarySizeBytes,
    binarySha256: $binarySha256,
    semanticResponseSha256: $semanticResponseSha256, semanticRuns: $semanticRuns,
    runtimeBaseDigest: $runtimeBaseDigest,
    buildInputsSha256: $buildInputsSha256,
    staticClosureSha256: $staticClosureSha256,
    staticClosureEntries: $staticClosureEntries,
    cargoClosureSha256: $cargoClosureSha256,
    licenseHashesSha256: $licenseHashesSha256, licenseFiles: $licenseFiles,
    linkActionSha256: $linkActionSha256,
    platformSourceHashesSha256: $platformSourceHashesSha256}' \
  > "$results_dir/candidate-metadata.json"
chmod 0444 "$results_dir/semantic-response.sha256" "$results_dir/candidate-metadata.json"

printf 'candidate_gate=pass architecture=%s binary_sha256=%s native_runs=%d\n' \
  "$native_arch" "$binary_sha" "$NATIVE_RUNS"
printf 'semantic_response_sha256=%s\n' "$semantic_sha"
if [[ $# -eq 2 ]]; then
  printf 'evidence_directory=%s\n' "$results_dir"
fi

#!/usr/bin/env bash
set -euo pipefail

EXPECTED_VERSION="${EXPECTED_VERSION:-1.15.1-unadmitted.1}"
EXPECTED_PLATFORM_REVISION="${EXPECTED_PLATFORM_REVISION:-WORKTREE}"
EXPECTED_UPSTREAM_REVISION='04024d701f79feb8e2f18bc3df0dffc04ef05088'
EXPECTED_AMD64_SDK_SHA='5b05b157e512390b6bcc14517f2c68736b89aae286ab89fd0666c3c80a1569bc'
EXPECTED_ARM64_SDK_SHA='82a2050617f6b7a3cc75ee2aa0c15855f683012fa9260391ef696db6cdf2267c'
EXPECTED_SOURCE_SHA='30132da2b21899c62f07c4d6566325a3b869a7da4ce61d8b25f833b38e1562b1'
EXPECTED_EVIDENCE_SHA='d8ea033afc2b95f7186e9b83c6892ba6cf6713df4d44660642a4f6699b98f95b'
MAX_IMAGE_BYTES=536870912
SEMANTIC_RUNS=20
FORBIDDEN_GO_DEPS='^(net|net/|net/http|crypto/tls|os/exec|google\.golang\.org/grpc|golang\.org/x/net)'
NETWORK_SANDBOX=(--network none --cap-drop ALL --security-opt no-new-privileges:true)
RESOURCE_SANDBOX=(--read-only --pids-limit 128 --memory 256m --cpus 1 --user 65532:0)

fail() {
  printf 'verify-highs-candidate: %s\n' "$*" >&2
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

verify_containerfile() {
  local path="$1"
  local final_from
  [[ -f "$path" ]] || fail "Containerfile does not exist: $path"
  require_text "$path" 'ubuntu:24.04@sha256:561618e2c15bf2397621dd04f96926663a3b5616c189cf7e38db7e82f5c538ea'
  require_text "$path" 'cgr.dev/chainguard/static:latest@sha256:399c8cb4858f05aaa33f43f02a2e75f28d40f016c0f86e5ba6075769e3303791'
  require_text "$path" 'golang:1.26.11-bookworm@sha256:dc40a13b61304647ed7e5d581d246b86413dfc29d63c6d018310feb93cd98fc0'
  require_text "$path" 'ARG TARGETARCH=amd64'
  require_text "$path" 'FROM scratch AS highs-sdk'
  require_text "$path" 'FROM ${BUILDER_IMAGE} AS build'
  require_text "$path" 'SHELL ["/bin/bash", "-o", "pipefail", "-c"]'
  require_text "$path" 'COPY --from=ca-bootstrap /etc/ssl/certs/ca-certificates.crt'
  require_text "$path" 'SNAPSHOT=20260811T000000Z'
  require_text "$path" 'g++-13=13.3.0-6ubuntu2~24.04.1'
  require_text "$path" 'libc6-dev=2.39-0ubuntu8.8'
  require_text "$path" 'zlib1g-dev=1:1.3.dfsg-3.1ubuntu2.1'
  require_text "$path" "ADD --checksum=sha256:${EXPECTED_AMD64_SDK_SHA}"
  require_text "$path" "ADD --checksum=sha256:${EXPECTED_ARM64_SDK_SHA}"
  require_text "$path" "ADD --checksum=sha256:${EXPECTED_SOURCE_SHA}"
  require_text "$path" 'source-archive.tar.gz'
  require_text "$path" "input.evidence.sha256=\"${EXPECTED_EVIDENCE_SHA}\""
  require_text "$path" 'CGO_ENABLED=1 GOOS=linux GOARCH="$TARGETARCH"'
  require_text "$path" 'go build -tags highs'
  require_text "$path" "-linkmode=external -extldflags '-static -static-libgcc -static-libstdc++"
  require_text "$path" '/out/evidence/linker.map'
  require_text "$path" '/out/evidence/static-closure.tsv'
  require_text "$path" '/out/evidence/license-hashes.sha256'
  require_text "$path" "'s#/tmp/go-link-[0-9]+#/tmp/go-link-STABLE#g'"
  require_text "$path" "'s#/tmp/go-build[0-9]+#/tmp/go-buildSTABLE#g'"
  require_text "$path" 'find /out -depth -exec touch --no-dereference --date="@${SOURCE_DATE_EPOCH}" {} +'
  require_text "$path" 'find /rootfs -depth -exec touch --no-dereference --date="@${SOURCE_DATE_EPOCH}" {} +'
  require_text "$path" 'io.palantir.platform.optimizer.admission="UNADMITTED"'
  require_text "$path" 'io.palantir.platform.optimizer.architecture=$TARGETARCH'
  require_text "$path" 'ARG REVISION=WORKTREE'
  require_text "$path" "io.palantir.platform.optimizer.upstream.sdk.amd64.sha256=\"${EXPECTED_AMD64_SDK_SHA}\""
  require_text "$path" "io.palantir.platform.optimizer.upstream.sdk.arm64.sha256=\"${EXPECTED_ARM64_SDK_SHA}\""
  require_text "$path" "io.palantir.platform.optimizer.upstream.commit=\"${EXPECTED_UPSTREAM_REVISION}\""
  require_text "$path" 'USER 65532:0'
  require_text "$path" 'ENTRYPOINT ["/platform-highs-shim"]'
  require_text "$path" 'external-solver-process/v1alpha3'
  require_text "$path" 'BSD-3-Clause'
  require_text "$path" 'LGPL-2.1-or-later'
  require_text "$path" 'GCC-exception-3.1'
  final_from="$(awk 'toupper($1) == "FROM" { line=$0 } END { print line }' "$path")"
  [[ "$final_from" == 'FROM scratch' ]] || fail "candidate final stage is not scratch: $final_from"
  [[ "$(grep -Fc 'COPY --from=build --chown=65532:0 /rootfs/ /' "$path")" -eq 1 ]] ||
    fail 'candidate must copy its normalized root filesystem exactly once'
}

verify_self_audit() {
  local path="$1"
  [[ -f "$path" ]] || fail "verifier does not exist: $path"
  grep -Fxq 'NETWORK_SANDBOX=(--network none --cap-drop ALL --security-opt no-new-privileges:true)' "$path" || fail 'verifier runtime network isolation was removed or changed'
  grep -Fxq 'RESOURCE_SANDBOX=(--read-only --pids-limit 128 --memory 256m --cpus 1 --user 65532:0)' "$path" || fail 'verifier runtime resource isolation was removed or changed'
  grep -Fxq "FORBIDDEN_GO_DEPS='^(net|net/|net/http|crypto/tls|os/exec|google\\.golang\\.org/grpc|golang\\.org/x/net)'" "$path" || fail 'verifier Go transport closure gate was removed or changed'
  require_text "$path" 'MAX_IMAGE_BYTES=536870912'
  require_text "$path" 'SEMANTIC_RUNS=20'
  # Matched as the readelf guard lines, not as bare grep fragments. The old
  # form searched for "grep -q 'INTERP'", which the require_text lines here
  # also carry -- so deleting the runtime checks still passed self-audit.
  grep -Fxq 'if readelf -l "$binary" | grep -q '\''INTERP'\''; then' "$path" ||
    fail 'verifier program-interpreter gate was removed or changed'
  grep -Fxq 'if readelf -d "$binary" 2>&1 | grep -q '\''NEEDED'\''; then' "$path" ||
    fail 'verifier dynamic-dependency gate was removed or changed'
  require_text "$path" 'license-hashes.sha256'
  require_text "$path" 'static-closure.tsv'
  require_text "$path" 'dormant-native-network-symbols.txt'
  require_text "$path" 'io.palantir.platform.optimizer.admission'
  require_text "$path" 'ExternalSolverProcessResponse'
  require_text "$path" 'docker run --rm -i'
  require_text "$path" 'candidate-metadata.json'
  require_text "$path" 'semantic-response.sha256'
  grep -Fxq '(( binary_size <= MAX_IMAGE_BYTES )) || fail "candidate executable exceeds 512 MiB"' "$path" || fail 'verifier executable-size gate was removed or changed'
}

host_architecture() {
  case "$(uname -m)" in
    x86_64) printf 'amd64\n' ;;
    aarch64|arm64) printf 'arm64\n' ;;
    *) fail "unsupported native host architecture: $(uname -m)" ;;
  esac
}

script_dir="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
platform_dir="$(CDPATH= cd -- "$script_dir/.." && pwd)"
containerfile="$platform_dir/Containerfile.optimizer-highs"
request_file="$script_dir/testdata/highs-candidate-request.json"

case "${1:-}" in
  --containerfile)
    [[ $# -eq 2 ]] || fail 'usage: verify-highs-candidate.sh --containerfile PATH'
    verify_containerfile "$2"
    printf 'containerfile_gate=pass\n'
    exit 0
    ;;
  --self-audit)
    [[ $# -le 2 ]] || fail 'usage: verify-highs-candidate.sh --self-audit [PATH]'
    verify_self_audit "${2:-${BASH_SOURCE[0]}}"
    printf 'verifier_self_audit=pass\n'
    exit 0
    ;;
esac

[[ $# -ge 1 && $# -le 2 ]] || fail 'usage: verify-highs-candidate.sh IMAGE [NEW_EVIDENCE_DIRECTORY]'
image="$1"

for command_name in docker jq file readelf nm sha256sum tar stat awk grep cmp mktemp install uname find; do
  require_command "$command_name"
done
verify_containerfile "$containerfile"
verify_self_audit "${BASH_SOURCE[0]}"
[[ -s "$request_file" ]] || fail "semantic request fixture is absent: $request_file"
jq -e 'type == "object"' "$request_file" >/dev/null || fail 'semantic request fixture is not valid JSON'

tmp_dir="$(mktemp -d -t verify-highs-candidate.XXXXXXXX)"
export_container=''
cleanup() {
  if [[ -n "$export_container" ]]; then
    docker rm -f "$export_container" >/dev/null 2>&1 || true
  fi
  if [[ -d "$tmp_dir" && "$tmp_dir" == /tmp/verify-highs-candidate.* ]]; then
    chmod -R u+w "$tmp_dir" 2>/dev/null || true
    rm -rf -- "$tmp_dir"
  fi
}
trap cleanup EXIT

results_dir="$tmp_dir/results"
if [[ $# -eq 2 ]]; then
  [[ ! -e "$2" ]] || fail "evidence directory already exists: $2"
  mkdir -p -- "$2"
  results_dir="$2"
else
  mkdir -p "$results_dir"
fi

docker image inspect "$image" > "$tmp_dir/inspect.json"
image_id="$(jq -er '.[0].Id' "$tmp_dir/inspect.json")"
image_size="$(jq -er '.[0].Size' "$tmp_dir/inspect.json")"
image_arch="$(jq -er '.[0].Architecture' "$tmp_dir/inspect.json")"
native_arch="$(host_architecture)"
[[ "$image_arch" == "$native_arch" ]] || fail "candidate architecture $image_arch is not native host architecture $native_arch"
case "$image_arch" in
  amd64)
    expected_sdk_sha="$EXPECTED_AMD64_SDK_SHA"
    expected_file_arch='x86-64'
    expected_elf_machine='Advanced Micro Devices X86-64'
    ;;
  arm64)
    expected_sdk_sha="$EXPECTED_ARM64_SDK_SHA"
    expected_file_arch='ARM aarch64'
    expected_elf_machine='AArch64'
    ;;
  *) fail "unsupported candidate architecture: $image_arch" ;;
esac
[[ "$image_size" =~ ^[0-9]+$ ]] || fail 'image size is not numeric'
(( image_size <= MAX_IMAGE_BYTES )) || fail "image is ${image_size} bytes; limit is ${MAX_IMAGE_BYTES}"

jq -e \
  --arg arch "$image_arch" \
  --arg version "$EXPECTED_VERSION" \
  --arg revision "$EXPECTED_PLATFORM_REVISION" \
  --arg upstream_revision "$EXPECTED_UPSTREAM_REVISION" \
  --arg amd64_sdk "$EXPECTED_AMD64_SDK_SHA" \
  --arg arm64_sdk "$EXPECTED_ARM64_SDK_SHA" \
  --arg source "$EXPECTED_SOURCE_SHA" \
  --arg evidence "$EXPECTED_EVIDENCE_SHA" '
    .[0].Architecture == $arch and
    .[0].Os == "linux" and
    .[0].Config.User == "65532:0" and
    .[0].Config.WorkingDir == "/" and
    .[0].Config.Entrypoint == ["/platform-highs-shim"] and
    .[0].Config.Labels["org.opencontainers.image.version"] == $version and
    .[0].Config.Labels["org.opencontainers.image.revision"] == $revision and
    .[0].Config.Labels["io.palantir.platform.optimizer.admission"] == "UNADMITTED" and
    .[0].Config.Labels["io.palantir.platform.optimizer.architecture"] == $arch and
    .[0].Config.Labels["io.palantir.platform.optimizer.protocol.request"] == "optimization.platform.example/v1alpha2" and
    .[0].Config.Labels["io.palantir.platform.optimizer.protocol.response"] == "optimization.platform.example/external-solver-process/v1alpha3" and
    .[0].Config.Labels["io.palantir.platform.optimizer.upstream.version"] == "1.15.1" and
    .[0].Config.Labels["io.palantir.platform.optimizer.upstream.commit"] == $upstream_revision and
    .[0].Config.Labels["io.palantir.platform.optimizer.upstream.sdk.amd64.sha256"] == $amd64_sdk and
    .[0].Config.Labels["io.palantir.platform.optimizer.upstream.sdk.arm64.sha256"] == $arm64_sdk and
    .[0].Config.Labels["io.palantir.platform.optimizer.upstream.source.sha256"] == $source and
    .[0].Config.Labels["io.palantir.platform.optimizer.input.evidence.sha256"] == $evidence and
    .[0].Config.Labels["io.palantir.platform.optimizer.ubuntu.snapshot"] == "20260811T000000Z"
  ' "$tmp_dir/inspect.json" >/dev/null || fail 'image config or annotations differ from the candidate contract'

export_container="$(docker create "$image")"
docker export -o "$tmp_dir/rootfs.tar" "$export_container"
mkdir "$tmp_dir/rootfs"
tar --exclude='dev/*' -xf "$tmp_dir/rootfs.tar" -C "$tmp_dir/rootfs"
binary="$tmp_dir/rootfs/platform-highs-shim"
evidence_dir="$tmp_dir/rootfs/usr/share/platform-highs/evidence"
licenses_dir="$tmp_dir/rootfs/usr/share/platform-highs/licenses"
source_dir="$tmp_dir/rootfs/usr/share/platform-highs/source"

[[ -f "$binary" && -x "$binary" ]] || fail 'candidate binary is absent or not executable'
[[ -d "$evidence_dir" && -d "$licenses_dir" && -d "$source_dir" ]] || fail 'candidate audit material is incomplete'
binary_size="$(stat -c '%s' "$binary")"
[[ "$binary_size" =~ ^[0-9]+$ ]] || fail 'candidate executable size is not numeric'
(( binary_size <= MAX_IMAGE_BYTES )) || fail "candidate executable exceeds 512 MiB"
file "$binary" | grep -Fq 'ELF 64-bit LSB executable' || fail 'candidate is not a 64-bit ELF executable'
file "$binary" | grep -Fq "$expected_file_arch" || fail "candidate is not $image_arch"
file "$binary" | grep -Fq 'statically linked' || fail 'candidate is not statically linked'
readelf -h "$binary" | grep -Fq "Machine:                           ${expected_elf_machine}" || fail 'candidate ELF machine disagrees with its image architecture'
if readelf -l "$binary" | grep -q 'INTERP'; then
  fail 'candidate has a program interpreter'
fi
if readelf -d "$binary" 2>&1 | grep -q 'NEEDED'; then
  fail 'candidate has a dynamic dependency'
fi
if find "$tmp_dir/rootfs" -xdev -mindepth 1 \( -type f -o -type d \) -perm /022 -print -quit | grep -q .; then
  fail 'candidate root filesystem contains group- or world-writable material'
fi

tar_entry="$(tar --numeric-owner -tvf "$tmp_dir/rootfs.tar" | awk '$NF == "platform-highs-shim" {print}')"
[[ "$tar_entry" == -r-xr-xr-x* ]] || fail "candidate binary mode is not 0555: $tar_entry"
[[ "$tar_entry" == *' 65532/0 '* ]] || fail "candidate binary ownership is not 65532:0: $tar_entry"

for required in \
  build-inputs.json linker.map static-closure.tsv go-deps.txt go-buildinfo.txt \
  dormant-native-network-symbols.txt dormant-native-network-symbol-count.txt license-hashes.sha256; do
  [[ -s "$evidence_dir/$required" ]] || fail "required evidence file is absent or empty: $required"
done

jq -e \
  --arg arch "$image_arch" \
  --arg sdk "$expected_sdk_sha" \
  --arg source "$EXPECTED_SOURCE_SHA" \
  --arg evidence "$EXPECTED_EVIDENCE_SHA" '
    .schemaVersion == 1 and .admission == "UNADMITTED" and .architecture == $arch and
    .ubuntuSnapshot == "20260811T000000Z" and
    .highsArchiveSha256 == $sdk and .highsSourceArchiveSha256 == $source and
    .evidenceDocumentSha256 == $evidence and
    .caBootstrapImageDigest == "sha256:399c8cb4858f05aaa33f43f02a2e75f28d40f016c0f86e5ba6075769e3303791" and
    (.runtimeDigest | not) and
    .compiler == "g++-13 13.3.0-6ubuntu2~24.04.1" and
    .libc6Dev == "2.39-0ubuntu8.8" and .zlibDev == "1:1.3.dfsg-3.1ubuntu2.1"
  ' "$evidence_dir/build-inputs.json" >/dev/null || fail 'build input manifest differs from pinned admission evidence'

for closure_text in \
  $'upstream-sdk\tERGO-Code/HiGHS\t1.15.1' \
  $'ubuntu-package\tzlib1g-dev\t1:1.3.dfsg-3.1ubuntu2.1' \
  $'ubuntu-package\tlibc6-dev\t2.39-0ubuntu8.8' \
  $'ubuntu-package\tlibstdc++-13-dev\t13.3.0-6ubuntu2~24.04.1' \
  $'ubuntu-package\tlibgcc-13-dev\t13.3.0-6ubuntu2~24.04.1'; do
  grep -Fq -- "$closure_text" "$evidence_dir/static-closure.tsv" || fail "static closure owner is absent: $closure_text"
done
grep -Fq 'libhighs.a' "$evidence_dir/linker.map" || fail 'linker map does not bind libhighs.a'
grep -Fq 'libhighs_extras.a' "$evidence_dir/linker.map" || fail 'linker map does not bind libhighs_extras.a'
grep -Fq 'libz.a' "$evidence_dir/linker.map" || fail 'linker map does not bind static zlib'
grep -Fq 'libstdc++.a' "$evidence_dir/linker.map" || fail 'linker map does not bind static libstdc++'
grep -Fq 'libc.a' "$evidence_dir/linker.map" || fail 'linker map does not bind static libc'

license_count="$(find "$licenses_dir" -maxdepth 1 -type f | wc -l | tr -d ' ')"
(( license_count >= 13 )) || fail "license closure has only ${license_count} files"
(
  cd "$licenses_dir"
  sha256sum -c "$evidence_dir/license-hashes.sha256" >/dev/null
) || fail 'license hash manifest does not match the final image'
for required_license in \
  HIGHS-SOURCE-LICENSE.txt HIGHS-THIRD_PARTY_NOTICES.md HIGHS-FILEREADERLP-LICENSE \
  HIGHS-PDQSORT-LICENSE.txt HIGHS-ZSTR-LICENSE UBUNTU-ZLIB-COPYRIGHT \
  UBUNTU-GLIBC-COPYRIGHT UBUNTU-GCC-RUNTIME-COPYRIGHT GO-LICENSE PLATFORM-LICENSE; do
  [[ -s "$licenses_dir/$required_license" ]] || fail "license closure lacks $required_license"
done

if grep -Eq "$FORBIDDEN_GO_DEPS" "$evidence_dir/go-deps.txt"; then
  fail 'Go dependency closure contains transport, TLS, or process execution authority'
fi
[[ "$(grep -Ec '^github.com/jonathanagustin/conduit/' "$evidence_dir/go-deps.txt")" -eq 3 ]] || fail 'unexpected Platform package entered the shim closure'
for required_package in \
  github.com/jonathanagustin/conduit/cmd/platform-highs-shim \
  github.com/jonathanagustin/conduit/internal/highsshim \
  github.com/jonathanagustin/conduit/internal/optimization/solverprotocol; do
  grep -Fxq "$required_package" "$evidence_dir/go-deps.txt" || fail "expected transport-free package is absent: $required_package"
done
if grep -RIE 'net/http|net\.Dial|socket[[:space:]]*\(|connect[[:space:]]*\(|os\.Getenv|os\.LookupEnv|os\.Args|internal/(controlplane|delivery|execution)|cmd/platform-(delivery|operator|spoke-agent)' "$source_dir"; then
  fail 'source closure contains transport, environment, argv, or delivery authority'
fi
if nm -u "$binary" | grep -Eq '[[:space:]](socket|socketpair|connect|accept|accept4|bind|listen|send|sendto|sendmsg|recv|recvfrom|recvmsg)($|@)'; then
  fail 'candidate has an undefined native network reference'
fi
if nm "$binary" | grep -E '(net/http|net\.Dial|golang\.org/x/net|crypto/tls)' > "$tmp_dir/go-network-symbols.txt"; then
  fail 'candidate contains a Go network or TLS symbol'
fi
nm --defined-only "$binary" | \
  awk '$NF ~ /^(socket|socketpair|connect|accept|accept4|bind|listen|send|sendto|sendmsg|recv|recvfrom|recvmsg)(@@.*)?$/ {print $NF}' | \
  LC_ALL=C sort -u > "$tmp_dir/dormant-native-network-symbols.txt"
cmp -s "$tmp_dir/dormant-native-network-symbols.txt" "$evidence_dir/dormant-native-network-symbols.txt" || fail 'dormant libc network symbol manifest differs from the binary'
dormant_count="$(wc -l < "$tmp_dir/dormant-native-network-symbols.txt" | tr -d ' ')"
[[ "$dormant_count" == "$(tr -d '[:space:]' < "$evidence_dir/dormant-native-network-symbol-count.txt")" ]] || fail 'dormant libc network symbol count is inconsistent'

for ((run = 1; run <= SEMANTIC_RUNS; run++)); do
  docker run --rm -i "${NETWORK_SANDBOX[@]}" "${RESOURCE_SANDBOX[@]}" "$image" \
      < "$request_file" > "$tmp_dir/semantic-${run}.json" 2> "$tmp_dir/semantic-${run}.stderr" ||
    fail "sandboxed semantic execution failed on run $run"
  [[ ! -s "$tmp_dir/semantic-${run}.stderr" ]] || fail "native diagnostics leaked on run $run"
  if (( run > 1 )); then
    cmp -s "$tmp_dir/semantic-1.json" "$tmp_dir/semantic-${run}.json" || fail "sandboxed semantic output changed on run $run"
  fi
done

jq -e '
  .apiVersion == "optimization.platform.example/external-solver-process/v1alpha3" and
  .kind == "ExternalSolverProcessResponse" and
  .requestId == "solreq_1a6fcbd3e4d91feb2a7704cc9708cc860beaf27ae6e61290dad2d047cd08e6fe" and
  .modelDigest == "sha256:7cd6354d2d7ff2f5aa7126e091783349da51b5f49767174cf792e8e2ab302f26" and
  .termination == "OPTIMAL" and
  .backendReportedEvidence.status == "kOptimal" and
  .backendReportedEvidence.objectives == [{"objective":"minimize_makespan","unit":"normalized_load_ratio","incumbent":"1","bestBound":"1","gap":"0"}] and
  (.assignments | length) == 2 and
  ([.assignments[].item] | sort) == ["first","second"] and
  ([.assignments[].resource] | sort) == ["a","b"]
' "$tmp_dir/semantic-1.json" >/dev/null || fail 'Platform semantic assertion rejected the real HiGHS response'

binary_sha="$(sha256sum "$binary" | cut -d' ' -f1)"
request_sha="$(sha256sum "$request_file" | cut -d' ' -f1)"
response_sha="$(sha256sum "$tmp_dir/semantic-1.json" | cut -d' ' -f1)"
build_inputs_sha="$(sha256sum "$evidence_dir/build-inputs.json" | cut -d' ' -f1)"
static_closure_sha="$(sha256sum "$evidence_dir/static-closure.tsv" | cut -d' ' -f1)"
static_closure_entries="$(awk 'NR > 1 { count++ } END { print count + 0 }' "$evidence_dir/static-closure.tsv")"
license_hashes_sha="$(sha256sum "$evidence_dir/license-hashes.sha256" | cut -d' ' -f1)"
linker_map_sha="$(sha256sum "$evidence_dir/linker.map" | cut -d' ' -f1)"
install -m 0444 "$tmp_dir/semantic-1.json" "$results_dir/semantic-response.json"
printf '%s  semantic-response.json\n' "$response_sha" > "$results_dir/semantic-response.sha256"
jq --null-input \
  --arg architecture "$image_arch" \
  --arg imageId "$image_id" \
  --arg binarySha256 "$binary_sha" \
  --arg requestSha256 "$request_sha" \
  --arg responseSha256 "$response_sha" \
  --arg buildInputsSha256 "$build_inputs_sha" \
  --arg staticClosureSha256 "$static_closure_sha" \
  --arg licenseHashesSha256 "$license_hashes_sha" \
  --arg linkerMapSha256 "$linker_map_sha" \
  --arg platformRevision "$EXPECTED_PLATFORM_REVISION" \
  --argjson imageSizeBytes "$image_size" \
  --argjson binarySizeBytes "$binary_size" \
  --argjson semanticRuns "$SEMANTIC_RUNS" \
  --argjson dormantNativeNetworkSymbols "$dormant_count" \
  --argjson staticClosureEntries "$static_closure_entries" \
  '{schemaVersion: 1, admission: "UNADMITTED", architecture: $architecture,
    platformRevision: $platformRevision, imageId: $imageId,
    imageSizeBytes: $imageSizeBytes, binarySizeBytes: $binarySizeBytes,
    binarySha256: $binarySha256, requestSha256: $requestSha256,
    semanticResponseSha256: $responseSha256, semanticRuns: $semanticRuns,
    dormantNativeNetworkSymbols: $dormantNativeNetworkSymbols,
    buildInputsSha256: $buildInputsSha256,
    staticClosureSha256: $staticClosureSha256,
    staticClosureEntries: $staticClosureEntries,
    licenseHashesSha256: $licenseHashesSha256,
    linkerMapSha256: $linkerMapSha256}' \
  > "$results_dir/candidate-metadata.json"
chmod 0444 "$results_dir/semantic-response.sha256" "$results_dir/candidate-metadata.json"

printf 'candidate_gate=pass\n'
printf 'architecture=%s\n' "$image_arch"
printf 'image=%s\n' "$image"
printf 'image_id=%s\n' "$image_id"
printf 'image_size_bytes=%s\n' "$image_size"
printf 'binary_size_bytes=%s\n' "$binary_size"
printf 'binary_sha256=%s\n' "$binary_sha"
printf 'semantic_response_sha256=%s\n' "$response_sha"
printf 'semantic_runs=%s\n' "$SEMANTIC_RUNS"
printf 'dormant_native_network_symbols=%s\n' "$dormant_count"
if [[ $# -eq 2 ]]; then
  printf 'evidence_directory=%s\n' "$results_dir"
fi

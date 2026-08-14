#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'verify-highs-candidate.test: %s\n' "$*" >&2
  exit 1
}

script_dir="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
platform_dir="$(CDPATH= cd -- "$script_dir/.." && pwd)"
verifier="$script_dir/verify-highs-candidate.sh"
scanner="$script_dir/scan-highs-candidate.sh"
containerfile="$platform_dir/Containerfile.optimizer-highs"
evidence="$platform_dir/docs/evidence/highs-1.15.1-shim-candidate-amd64.json"
security_evidence="$platform_dir/docs/evidence/highs-1.15.1-shim-candidate-amd64-security"
workflow="$platform_dir/.github/workflows/platform-highs-candidate.yml"
dockerignore="$platform_dir/.dockerignore"

bash -n "$verifier"
bash -n "$scanner"
bash -n "${BASH_SOURCE[0]}"
"$verifier" --containerfile "$containerfile" >/dev/null
"$verifier" --self-audit "$verifier" >/dev/null
for required_context_input in \
  '!LICENSE' \
  '!NOTICE' \
  '!docs/evidence/highs-1.15.1-release.json'; do
  grep -Fxq -- "$required_context_input" "$dockerignore" ||
    fail "Docker context excludes required candidate input: $required_context_input"
done
jq -e \
  --arg containerfile "$(sha256sum "$containerfile" | cut -d' ' -f1)" \
  --arg verifier "$(sha256sum "$verifier" | cut -d' ' -f1)" \
  --arg scanner "$(sha256sum "$scanner" | cut -d' ' -f1)" \
  --arg securityMetadata "$(sha256sum "$security_evidence/security-metadata.json" | cut -d' ' -f1)" '
    .schemaVersion == 1 and .admission == "UNADMITTED" and .architecture == "amd64" and
    .platformRevision == "WORKTREE" and
    .source.containerfileSha256 == $containerfile and .source.verifierSha256 == $verifier and
    .source.scannerSha256 == $scanner and
    .candidate.semanticRuns == 20 and .candidate.dormantNativeNetworkSymbols == 4 and
    .candidate.staticClosureEntries == 15 and
    .reproducibility.cachedImageId == .reproducibility.cleanImageId and
    .reproducibility.byteIdenticalImage == true and
    .security.metadataSha256 == $securityMetadata and
    .security.tools == {syft: "1.50.0", grype: "0.116.1"} and
    .security.vulnerabilityMatches == 0 and .security.failThreshold == "high" and
    (.verifiedGates | length) == 12 and (.openAdmissionGates | length) == 4
  ' "$evidence" >/dev/null || fail 'committed amd64 candidate evidence is stale or malformed'
jq -e \
  --arg syft "$(sha256sum "$security_evidence/highs-amd64.syft.json" | cut -d' ' -f1)" \
  --arg spdx "$(sha256sum "$security_evidence/highs-amd64.spdx.json" | cut -d' ' -f1)" \
  --arg grype "$(sha256sum "$security_evidence/highs-amd64.grype.json" | cut -d' ' -f1)" '
    .admission == "UNADMITTED" and .architecture == "amd64" and
    .sbom.nativeSha256 == $syft and .sbom.spdxSha256 == $spdx and
    .vulnerability.reportSha256 == $grype and
    .vulnerability.grypeExitStatus == 0 and .vulnerability.database.valid == true
  ' "$security_evidence/security-metadata.json" >/dev/null ||
  fail 'committed amd64 security evidence is stale or malformed'

for required in \
  'actions/cache@27d5ce7f107fe9357f9df03efb73ab90386fccae' \
  '--build-arg "SOURCE_DATE_EPOCH=1786406400"' \
  'syft_1.50.0_linux_${ARCHITECTURE}.tar.gz' \
  'grype_0.116.1_linux_${ARCHITECTURE}.tar.gz' \
  '887c57cbcc2d0e8c5c110a4571a3fc7150058b24d74f993ee4663516e5c8ce86' \
  'a8d7504a149629324eb5f4ce3dc25dfd211bbfe047e64ee2bf7844b466c3d84d' \
  './scripts/scan-highs-candidate.sh'; do
  grep -Fq -- "$required" "$workflow" || fail "candidate workflow omits supply-chain contract: $required"
done

tmp_dir="$(mktemp -d -t verify-highs-candidate-test.XXXXXXXX)"
cleanup() {
  if [[ -d "$tmp_dir" && "$tmp_dir" == /tmp/verify-highs-candidate-test.* ]]; then
    rm -rf -- "$tmp_dir"
  fi
}
trap cleanup EXIT

rejected=0
expect_rejected() {
  local name="$1"
  shift
  if "$@" >"$tmp_dir/${name}.stdout" 2>"$tmp_dir/${name}.stderr"; then
    fail "mutation unexpectedly passed: $name"
  fi
  rejected=$((rejected + 1))
}

cp "$containerfile" "$tmp_dir/admitted.Containerfile"
sed -i 's/io\.palantir\.platform\.optimizer\.admission="UNADMITTED"/io.palantir.platform.optimizer.admission="ADMITTED"/' "$tmp_dir/admitted.Containerfile"
expect_rejected admitted "$verifier" --containerfile "$tmp_dir/admitted.Containerfile"

cp "$containerfile" "$tmp_dir/unpinned-sdk.Containerfile"
sed -i 's/5b05b157e512390b6bcc14517f2c68736b89aae286ab89fd0666c3c80a1569bc/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/g' "$tmp_dir/unpinned-sdk.Containerfile"
expect_rejected unpinned-sdk "$verifier" --containerfile "$tmp_dir/unpinned-sdk.Containerfile"

cp "$containerfile" "$tmp_dir/unpinned-arm64-sdk.Containerfile"
sed -i 's/82a2050617f6b7a3cc75ee2aa0c15855f683012fa9260391ef696db6cdf2267c/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb/g' "$tmp_dir/unpinned-arm64-sdk.Containerfile"
expect_rejected unpinned-arm64-sdk "$verifier" --containerfile "$tmp_dir/unpinned-arm64-sdk.Containerfile"

cp "$containerfile" "$tmp_dir/hardcoded-architecture.Containerfile"
sed -i 's/io\.palantir\.platform\.optimizer\.architecture=$TARGETARCH/io.palantir.platform.optimizer.architecture="amd64"/' "$tmp_dir/hardcoded-architecture.Containerfile"
expect_rejected hardcoded-architecture "$verifier" --containerfile "$tmp_dir/hardcoded-architecture.Containerfile"

cp "$containerfile" "$tmp_dir/moving-snapshot.Containerfile"
sed -i 's/SNAPSHOT=20260811T000000Z/SNAPSHOT=latest/' "$tmp_dir/moving-snapshot.Containerfile"
expect_rejected moving-snapshot "$verifier" --containerfile "$tmp_dir/moving-snapshot.Containerfile"

cp "$containerfile" "$tmp_dir/root-user.Containerfile"
sed -i 's/^USER 65532:0$/USER 0:0/' "$tmp_dir/root-user.Containerfile"
expect_rejected root-user "$verifier" --containerfile "$tmp_dir/root-user.Containerfile"

cp "$containerfile" "$tmp_dir/dynamic-link.Containerfile"
sed -i 's/-static -static-libgcc -static-libstdc++/-static-libgcc -static-libstdc++/' "$tmp_dir/dynamic-link.Containerfile"
expect_rejected dynamic-link "$verifier" --containerfile "$tmp_dir/dynamic-link.Containerfile"

cp "$containerfile" "$tmp_dir/no-license-map.Containerfile"
sed -i 's#/out/evidence/license-hashes\.sha256#/out/evidence/license-hashes.removed#g' "$tmp_dir/no-license-map.Containerfile"
expect_rejected no-license-map "$verifier" --containerfile "$tmp_dir/no-license-map.Containerfile"

cp "$containerfile" "$tmp_dir/duplicate-evidence.Containerfile"
sed -i '\#COPY --from=build --chown=65532:0 /rootfs/ /#p' "$tmp_dir/duplicate-evidence.Containerfile"
expect_rejected duplicate-evidence "$verifier" --containerfile "$tmp_dir/duplicate-evidence.Containerfile"

cp "$containerfile" "$tmp_dir/random-linker-path.Containerfile"
sed -i 's#/tmp/go-buildSTABLE#/tmp/go-buildUNSTABLE#' "$tmp_dir/random-linker-path.Containerfile"
expect_rejected random-linker-path "$verifier" --containerfile "$tmp_dir/random-linker-path.Containerfile"

cp "$containerfile" "$tmp_dir/variable-mtime.Containerfile"
sed -i 's#find /rootfs -depth -exec touch --no-dereference --date="@${SOURCE_DATE_EPOCH}" {} +#find /rootfs -depth -exec touch {} +#' "$tmp_dir/variable-mtime.Containerfile"
expect_rejected variable-mtime "$verifier" --containerfile "$tmp_dir/variable-mtime.Containerfile"

cp "$verifier" "$tmp_dir/network-enabled-verifier.sh"
sed -i 's/^NETWORK_SANDBOX=(--network none --cap-drop ALL --security-opt no-new-privileges:true)$/NETWORK_SANDBOX=(--network bridge --cap-drop ALL --security-opt no-new-privileges:true)/' "$tmp_dir/network-enabled-verifier.sh"
expect_rejected network-enabled "$tmp_dir/network-enabled-verifier.sh" --self-audit "$tmp_dir/network-enabled-verifier.sh"

cp "$verifier" "$tmp_dir/unbounded-cpu-verifier.sh"
sed -i 's/^RESOURCE_SANDBOX=(--read-only --pids-limit 128 --memory 256m --cpus 1 --user 65532:0)$/RESOURCE_SANDBOX=(--read-only --pids-limit 128 --memory 256m --user 65532:0)/' "$tmp_dir/unbounded-cpu-verifier.sh"
expect_rejected unbounded-cpu "$tmp_dir/unbounded-cpu-verifier.sh" --self-audit "$tmp_dir/unbounded-cpu-verifier.sh"

cp "$verifier" "$tmp_dir/go-transport-verifier.sh"
sed -i 's#^FORBIDDEN_GO_DEPS=.*#FORBIDDEN_GO_DEPS='"'"'^os/exec'"'"'#' "$tmp_dir/go-transport-verifier.sh"
expect_rejected go-transport "$tmp_dir/go-transport-verifier.sh" --self-audit "$tmp_dir/go-transport-verifier.sh"

printf '{}\n' > "$tmp_dir/minimal-candidate-metadata.json"
mkdir "$tmp_dir/minimal-candidate"
mv "$tmp_dir/minimal-candidate-metadata.json" "$tmp_dir/minimal-candidate/candidate-metadata.json"
printf '#!/usr/bin/env bash\nprintf '\''{"application":"syft","version":"0.0.0"}\\n'\''\n' > "$tmp_dir/wrong-syft"
printf '#!/usr/bin/env bash\nprintf '\''{"application":"grype","version":"0.116.1"}\\n'\''\n' > "$tmp_dir/right-grype"
chmod 0755 "$tmp_dir/wrong-syft" "$tmp_dir/right-grype"
expect_rejected wrong-syft env SYFT_BIN="$tmp_dir/wrong-syft" GRYPE_BIN="$tmp_dir/right-grype" \
  "$scanner" unavailable "$tmp_dir/minimal-candidate" "$tmp_dir/wrong-syft-output"

printf '#!/usr/bin/env bash\nprintf '\''{"application":"syft","version":"1.50.0"}\\n'\''\n' > "$tmp_dir/right-syft"
chmod 0755 "$tmp_dir/right-syft"
jq --null-input \
  '{architecture:"amd64",imageId:"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
    platformRevision:"WORKTREE",staticClosureSha256:"not-a-digest",staticClosureEntries:15}' \
  > "$tmp_dir/minimal-candidate/candidate-metadata.json"
expect_rejected invalid-static-closure env SYFT_BIN="$tmp_dir/right-syft" GRYPE_BIN="$tmp_dir/right-grype" \
  "$scanner" unavailable "$tmp_dir/minimal-candidate" "$tmp_dir/invalid-static-closure-output"

printf 'mutation_gate=pass\n'
printf 'mutations_rejected=%s\n' "$rejected"

#!/usr/bin/env bash
# Runs every gate .github/workflows/platform-ci.yml runs, on this machine.
#
# There was no way to do this before. Reproducing the gate set by hand takes a
# dozen commands and two pinned binary downloads, so in practice nobody did,
# and an unformatted file sat on main through eighteen merges (#42).
#
# Two things are read out of the workflow rather than restated here, because a
# second copy of them is exactly how a local runner starts lying:
#
#   - the pinned tool versions and their checksums
#   - the list of step names, checked against what this script covers, so a new
#     CI step fails here until it is either run or explicitly excused
#
# The workflow and the code now share the Conduit repository root. Keep their
# paths coupled so extraction into a standalone repository cannot make local
# verification measure a different tree than the GitHub workflow.
set -euo pipefail

# repo_root is where the gates run; workflow_root is where GitHub reads from.
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
workflow_root="$(git -C "${repo_root}" rev-parse --show-toplevel 2>/dev/null || echo "${repo_root}")"
workflow="${workflow_root}/.github/workflows/platform-ci.yml"
if [[ ! -f "${workflow}" ]]; then
  echo "cannot find ${workflow}; this script can no longer tell what CI runs" >&2
  exit 1
fi
# Keep the default cache with the checkout. Codespaces may offload HOME caches
# through symlinks into /tmp, whose targets disappear on restart. The repository
# ignores .cache/, while every downloaded tool is still authenticated against
# the URL and checksum pinned by CI before use.
cache="${PLATFORM_VERIFY_CACHE:-${repo_root}/.cache/platform-verify}"

cd "${repo_root}"
./scripts/repair-volatile-cache-links.sh
mkdir -p "${cache}"
export XDG_CACHE_HOME="${cache}/xdg"
mkdir -p "${XDG_CACHE_HOME}"
# Browser binaries are large, reproducible artifacts. Keep them off the
# constrained Codespaces workspace volume; `test-console.sh` installs the
# lock-selected Chromium whenever this volatile target disappeared on restart.
export PLAYWRIGHT_BROWSERS_PATH="${PLATFORM_PLAYWRIGHT_BROWSERS_PATH:-/tmp/workspace-offload/palantir-platform/playwright}"
mkdir -p "${PLAYWRIGHT_BROWSERS_PATH}"

only="${1:-}"
failures=()
skipped=()

# --- reading ci.yml -----------------------------------------------------

# The URL and checksum of a pinned tool, taken from the install step that
# writes it. Empty output means ci.yml no longer looks the way this expects,
# which is a hard error rather than a silent fall back to some other version.
pinned_url() {
  grep -oE 'https://[^[:space:]]+' "${workflow}" | grep -F "$1" | head -1
}
pinned_sha() {
  grep -oE "[0-9a-f]{64}  $1" "${workflow}" | head -1 | cut -d' ' -f1
}

fetch_pinned() {
  local name="$1" url_match="$2" ci_path="$3" destination="${cache}/$4"
  local url sha
  url="$(pinned_url "${url_match}")"
  sha="$(pinned_sha "${ci_path}")"
  if [[ -z "${url}" || -z "${sha}" ]]; then
    echo "cannot find the pinned ${name} in ${workflow##*/}; its install step has changed shape" >&2
    exit 1
  fi
  if [[ ! -e "${destination}" ]] || ! echo "${sha}  ${destination}" | sha256sum --check --status; then
    curl --fail --silent --show-error --location "${url}" --output "${destination}"
    echo "${sha}  ${destination}" | sha256sum --check --status
  fi
  printf '%s' "${destination}"
}

# --- running a gate -----------------------------------------------------

gate() {
  local name="$1"
  shift
  if [[ -n "${only}" && "${name}" != *"${only}"* ]]; then
    return 0
  fi
  printf '%-46s' "${name}"
  local output
  if output="$("$@" 2>&1)"; then
    echo 'ok'
  else
    echo 'FAILED'
    printf '%s\n' "${output}" | sed 's/^/    /'
    failures+=("${name}")
  fi
}

# --- pinned tooling -----------------------------------------------------
# CI installs helm to /usr/local/bin; here it goes in front of PATH instead, so
# an ambient helm of a different version cannot be what the tests measure. The
# version is pinned for a reason: the post-render test is sensitive to it.

if [[ -z "${only}" || "${only}" == *helm* || "${only}" == *Test* ]]; then
  helm_archive="$(fetch_pinned helm get.helm.sh/helm-v /tmp/helm.tar.gz helm.tar.gz)"
  if [[ ! -x "${cache}/helm" ]]; then
    tar -xzf "${helm_archive}" -C "${cache}" --strip-components=1 linux-amd64/helm
  fi
fi
hadolint_bin="$(fetch_pinned hadolint hadolint/hadolint/releases /tmp/hadolint hadolint)"
zarf_bin="$(fetch_pinned zarf zarf-dev/zarf/releases /tmp/zarf zarf)"
chmod 0755 "${hadolint_bin}" "${zarf_bin}" 2>/dev/null || true
[[ -e "${cache}/helm" ]] && chmod 0755 "${cache}/helm" && export PATH="${cache}:${PATH}"

# --- the gates, in ci.yml order -----------------------------------------

check_formatting() {
  local unformatted
  unformatted="$(gofmt -l .)"
  [[ -z "${unformatted}" ]] || { echo "not gofmt-clean:"; echo "${unformatted}"; return 1; }
}

oci_manifest() {
  ./scripts/assemble-oci-manifest.sh "${cache}/oci-manifest.json" \
    testdata/oci/hub.json testdata/oci/spoke-agent.json
  jq --exit-status '.apiVersion == "platform.delivery/oci-release/v2" and .version == "1.2.3" and (.images | length == 2) and all(.images[]; (.sboms | length == 2) and (.vulnerabilityReports | length == 2))' \
    "${cache}/oci-manifest.json" >/dev/null
}

supply_chain() {
  ruby scripts/verify-trusted-supply-chain.rb && ./scripts/test-retry-keyless-signing.sh
}

optimizer_candidate_release() {
  ruby scripts/verify-optimizer-candidate-release-workflow.rb &&
    ./scripts/verify-optimizer-candidate-release-workflow.test.sh &&
    ./scripts/assemble-optimizer-candidate-gate.test.sh
}

build_commands() {
  local command
  for command in platform-delivery platform-operator platform-spoke-agent \
    platform-module-publisher platform-grype-scanner conduit; do
    go build -o /dev/null "./cmd/${command}" || return 1
  done
}

apollo_contract() {
  ruby scripts/apollo-docs/validate-public-api.rb \
    docs/apollo/snapshots/apollo-openapi-c663874bd7467a01e719d039509b0f206d05fe2c/api-definition.yml &&
    ./scripts/verify-apollo-routes.sh &&
    ruby scripts/apollo-docs/verify-public-api-adapter.rb &&
    ruby scripts/apollo-docs/validate-cli-fixtures.rb &&
    ruby scripts/apollo-docs/validate-module-definitions.rb &&
    ./scripts/verify-foundry-interop.sh &&
    node scripts/verify-conduit-cli-contract.mjs &&
    node scripts/verify-foundry-audit3-capture.mjs &&
    node scripts/verify-audit3-contract.mjs &&
    node scripts/verify-apollo-rosetta-inventory.mjs &&
    node scripts/verify-rosetta-release-notes.mjs &&
    node scripts/verify-no-tenant-origins.mjs &&
    node scripts/verify-secondary-apollo-evidence.mjs &&
    node scripts/apollo-graphql/verify-observed-schema.mjs &&
    ./scripts/verify-apollo-graphql.test.sh &&
    node scripts/apollo-graphql/verify-apollo-interface.mjs &&
    ./scripts/verify-apollo-interface.test.sh &&
    node scripts/verify-documentation-freshness.mjs &&
    ./scripts/verify-documentation-freshness.test.sh
}

zenith_protocol() {
  node scripts/verify-zenith-protocol.mjs &&
    node tools/zenith-protocol/scripts/conform.test.mjs &&
    node tools/zenith-protocol/scripts/verify-evaluator-trial.test.mjs &&
    node tools/zenith-protocol/skills/review-writing/scripts/review-writing.test.mjs &&
    node tools/zenith-protocol/scripts/conform.mjs \
      --config .zenith-protocol.json \
      --profile structural \
      --enforcement standard
}

gate 'Check formatting' check_formatting
gate 'Validate tenant fidelity policy' ./scripts/verify-pr-fidelity.test.sh
gate 'Test' env PLATFORM_REQUIRE_HELM=1 go test -race ./...
gate 'Validate mutation harness' ./scripts/verify-mutant-killed.test.sh
gate 'Validate mutation scores' bash -c './scripts/verify-mutation-scores.test.sh && node scripts/verify-mutation-scores.mjs'
gate 'Validate PostgreSQL repository' ./scripts/test-postgres-repository.sh
gate 'Vet' go vet ./...
gate 'Validate operator console' ./scripts/test-console.sh
gate 'Validate console contract' bash -c './scripts/verify-console-contract.test.sh && node scripts/verify-console-contract.mjs'
# staticcheck and govulncheck are built against a specific Go release and
# refuse a module newer than it. Pin the analysis toolchain here rather than
# lowering `toolchain` in go.mod, which would also pull the digest-pinned
# container builds off go1.26.6 and bypass their supply-chain pinning.
gate 'Scan for known vulnerabilities' env GOTOOLCHAIN=go1.25.13 go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...
gate 'Lint' env GOTOOLCHAIN=go1.25.13 go run honnef.co/go/tools/cmd/staticcheck@v0.7.0 ./...
gate 'Validate Renovate configuration' jq --exit-status . renovate.json
gate 'Lint Containerfiles' env HADOLINT_BIN="${hadolint_bin}" ./scripts/verify-containerfiles.sh
gate 'Validate OCI release manifest assembly' oci_manifest
gate 'Validate Gitless runtime source descriptor' node scripts/render-runtime-source-descriptor.test.mjs
gate 'Validate version and tag agreement' ./scripts/verify-version-matches-tag.sh
gate 'Validate controlplane ceiling' node scripts/report-migration-progress.mjs --gate
gate 'Validate domain boundaries' bash -c 'node scripts/verify-domain-boundaries.mjs && node scripts/test-domain-boundaries.mjs'
gate 'Validate trusted supply-chain policy' supply_chain
gate 'Validate optimizer candidate release policy' optimizer_candidate_release
gate 'Validate Rubix substrate CRDs' ruby scripts/verify-rubix-crds.rb
gate 'Validate Flux runtime source' ruby scripts/verify-flux-runtime.rb
gate 'Validate Helm runtime chart' ruby scripts/verify-helm-runtime.rb
gate 'Validate Zarf air-gap package' env ZARF_BIN="${zarf_bin}" ruby scripts/verify-zarf-package.rb
gate 'Build commands' build_commands
gate 'Validate Apollo public contract' apollo_contract
gate 'Validate Zenith protocol ledgers' zenith_protocol
gate 'Validate Module publication' go run ./cmd/platform-module-publisher --directory examples/modules --dry-run
gate 'GitOps acceptance' ./scripts/test-gitops.sh

# --- drift against ci.yml -----------------------------------------------
# Every step above is named exactly as ci.yml names it. The setup steps are not
# run as written because GitHub Actions owns their placement. `test-console.sh`
# still installs the lock-selected browser into this verifier's isolated cache;
# CI's browser step additionally installs runner OS dependencies.

not_run_as_written=(
  'Install pinned Helm'
  'Install pinned pnpm'
  'Install pinned Node'
  'Install console dependencies'
  'Install pinned console browser'
  'Install pinned Hadolint'
  'Install pinned Zarf'
  'Publish pending Trunk status'
  'Publish gate summary'
  'Publish final Trunk status'
)
mapfile -t ci_steps < <(sed -n 's/^      - name: //p' "${workflow}")
if ((${#ci_steps[@]} == 0)); then
  echo "no steps found in ${workflow##*/}; this script can no longer tell what CI runs" >&2
  exit 1
fi
for step in "${ci_steps[@]}"; do
  covered=false
  for known in "${not_run_as_written[@]}"; do
    [[ "${step}" == "${known}" ]] && covered=true
  done
  grep -qF "gate '${step}'" "${BASH_SOURCE[0]}" && covered=true
  "${covered}" || skipped+=("${step}")
done

echo
if ((${#skipped[@]})); then
  printf 'ci.yml runs steps this script does not: %s\n' "$(printf '%s; ' "${skipped[@]}")" >&2
  echo 'add them above, or list them in not_run_as_written with a reason.' >&2
  exit 1
fi
if ((${#failures[@]})); then
  printf 'failed: %s\n' "$(printf '%s; ' "${failures[@]}")" >&2
  exit 1
fi
[[ -n "${only}" ]] && echo "gates matching '${only}' passed" || echo 'all CI gates passed locally'

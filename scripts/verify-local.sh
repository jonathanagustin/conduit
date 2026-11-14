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
# Both readings are deliberately job-agnostic: the workflow runs its gates in
# four parallel lanes (#213), and which lane a gate sits in is a scheduling
# decision, while whether it runs at all is the thing this script exists to
# reconcile. A gate moved between lanes therefore changes nothing here; a gate
# deleted from the workflow still fails the reconciliation below.
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

release_path() {
  ruby scripts/verify-release-workflow.rb && ./scripts/verify-release-workflow.test.sh && ./scripts/test-release-preparation.sh
}

build_commands() {
  local command
  for command in platform-delivery platform-operator platform-spoke-agent \
    platform-module-publisher platform-grype-scanner packaging-workflow conduit; do
    go build -o /dev/null "./cmd/${command}" || return 1
  done
}

apollo_contract() {
  # The response-shape check leads, because the gates below it establish that
  # the transport matches the snapshot and none of them reads a response body.
  # It drives the read operations and validates what they answer with.
  go test ./internal/controlplane -run TestApolloResponsesMatchThePinnedSchemas &&
    ruby scripts/apollo-docs/validate-public-api.rb \
      docs/reference-platform/snapshots/reference-openapi-c663874bd7467a01e719d039509b0f206d05fe2c/api-definition.yml &&
    ./scripts/verify-apollo-routes.sh &&
    ruby scripts/apollo-docs/verify-public-api-adapter.rb &&
    ruby scripts/apollo-docs/validate-cli-fixtures.rb &&
    ruby scripts/apollo-docs/validate-module-definitions.rb &&
    ./scripts/verify-foundry-interop.sh &&
    node scripts/verify-conduit-cli-contract.mjs &&
    ./scripts/verify-conduit-cli-contract.test.sh &&
    node scripts/verify-foundry-audit3-capture.mjs &&
    node scripts/verify-audit3-contract.mjs &&
    node scripts/verify-apollo-rosetta-inventory.mjs &&
    node scripts/verify-rosetta-release-notes.mjs &&
    node scripts/verify-apollo-control-panel-seam.mjs &&
    node scripts/verify-apollo-identifier-grammar.mjs &&
    node scripts/verify-conduit-inventions.mjs &&
    node scripts/verify-notification-types.mjs &&
    node scripts/verify-minted-values.mjs &&
    node scripts/verify-documentation-claims.mjs &&
    node scripts/verify-no-tenant-origins.mjs &&
    node scripts/verify-secondary-apollo-evidence.mjs &&
    node scripts/verify-apollo-authorization-bounds.mjs &&
    ./scripts/verify-apollo-authorization-bounds.test.sh &&
    node scripts/apollo-graphql/verify-observed-schema.mjs &&
    ./scripts/verify-apollo-graphql.test.sh &&
    node scripts/apollo-graphql/verify-apollo-interface.mjs &&
    ./scripts/verify-apollo-interface.test.sh &&
    node scripts/verify-documentation-freshness.mjs &&
    ./scripts/verify-documentation-freshness.test.sh
}

review_ledger() {
  node scripts/verify-review-ledger.mjs &&
    ./scripts/verify-review-ledger.test.sh
}

gate 'Check formatting' check_formatting
# The runnable half of CI's head-authentication step. The authenticator itself
# reads the run's own GitHub event and asks the API about a pull request, so it
# has nothing to authenticate here; its self-test stubs both and is the part
# that would fail if a refusal stopped refusing.
gate 'Authenticate pull request head' ./scripts/authenticate-pull-request-head.test.sh
gate 'Validate commit metadata' bash -c 'node scripts/verify-commit-metadata.test.mjs && node scripts/verify-commit-metadata.mjs'
gate 'Validate tenant fidelity policy' ./scripts/verify-pr-fidelity.test.sh
# The other runnable half of that same CI step: the read that feeds the verifier.
# Separate from the line above because they fail differently and only one of them
# is about the pull request body.
gate 'Read pull request body' ./scripts/read-pull-request-body.test.sh
gate 'Validate GitHub API retry' ./scripts/test-retry-github-api.sh
gate 'Test' env PLATFORM_REQUIRE_HELM=1 go test -race ./...
gate 'Validate mutation harness' ./scripts/verify-mutant-killed.test.sh
gate 'Validate packaging regression gates' python3 scripts/test-packaging-gates.py
gate 'Validate packaging endpoints' ./scripts/verify-packaging-endpoints.sh
gate 'Validate mutation scores' bash -c './scripts/verify-mutation-scores.test.sh && node scripts/verify-mutation-scores.mjs'
gate 'Validate PostgreSQL repository' ./scripts/test-postgres-repository.sh
gate 'Vet' go vet ./...
gate 'Validate standalone CLI release targets' ./scripts/verify-cli-release-targets.sh
gate 'Validate operator console' ./scripts/test-console.sh
gate 'Validate console contract' bash -c './scripts/verify-console-contract.test.sh && node scripts/verify-console-contract.mjs'
gate 'Validate design tokens against the captured product' bash -c './scripts/verify-design-tokens.test.sh && node scripts/verify-design-tokens.mjs'
gate 'Validate console CSS variables' bash -c './scripts/verify-console-css-variables.test.sh && node scripts/verify-console-css-variables.mjs'
gate 'Validate console boundaries' bash -c 'node scripts/verify-console-boundaries.test.mjs && node scripts/verify-console-boundaries.mjs'
# Keep the scanners on the same patched compiler used by production builds.
gate 'Scan for known vulnerabilities' env GOTOOLCHAIN=go1.26.11 go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...
gate 'Lint' env GOTOOLCHAIN=go1.26.11 go run honnef.co/go/tools/cmd/staticcheck@v0.8.2 ./...
gate 'Validate Renovate configuration' jq --exit-status . renovate.json
gate 'Lint Containerfiles' env HADOLINT_BIN="${hadolint_bin}" ./scripts/verify-containerfiles.sh
gate 'Validate OCI release manifest assembly' oci_manifest
gate 'Validate Gitless runtime source descriptor' node scripts/render-runtime-source-descriptor.test.mjs
self_product_preparation() {
  node scripts/prepare-self-products.test.mjs && node scripts/verify-conduit-inventions.test.mjs
}
gate 'Validate self Product preparation' self_product_preparation
gate 'Validate version and tag agreement' ./scripts/verify-version-matches-tag.sh
gate 'Validate domain boundaries' bash -c 'node scripts/verify-domain-boundaries.mjs && node scripts/test-domain-boundaries.mjs'
gate 'Validate route authorization' bash -c 'node scripts/verify-route-authorization.test.mjs && node scripts/verify-route-authorization.mjs'
gate 'Validate trusted supply-chain policy' supply_chain
gate 'Validate optimizer candidate release policy' optimizer_candidate_release
gate 'Validate release path' release_path
gate 'Validate Rubix substrate CRDs' ruby scripts/verify-rubix-crds.rb
gate 'Validate Flux runtime source' ruby scripts/verify-flux-runtime.rb
gate 'Validate Helm runtime chart' ruby scripts/verify-helm-runtime.rb
gate 'Validate Zarf air-gap package' env ZARF_BIN="${zarf_bin}" ruby scripts/verify-zarf-package.rb
gate 'Build commands' build_commands
gate 'Validate Apollo public contract' apollo_contract
gate 'Validate review ledger' review_ledger
gate 'Validate Apollo frontend surface' bash -c 'node scripts/verify-apollo-frontend-surface.mjs && ./scripts/verify-apollo-frontend-surface.test.sh'
gate 'Validate Module publication' go run ./cmd/platform-module-publisher --directory examples/modules --dry-run
gate 'GitOps acceptance' ./scripts/test-gitops.sh

# --- drift against ci.yml -----------------------------------------------
# Every step above is named exactly as ci.yml names it. The setup steps are not
# run as written because GitHub Actions owns their placement. `test-console.sh`
# still installs the lock-selected browser into this verifier's isolated cache;
# CI's browser step additionally installs runner OS dependencies.

not_run_as_written=(
  # Hosted scheduling only; the local verifier always runs the full gate set.
  'Classify changed paths'
  'Skip when no Go-relevant paths changed'
  'Skip when no relevant paths changed'
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
  'Conclude the Trunk gate job with its own verdict'
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
# --- drift the other way ------------------------------------------------
# The reconciliation above walks ci.yml -> this script and asks whether each CI
# step is covered here. Nothing walked the other way, and the gap that opened in
# it was not small: fourteen Apollo gates ran here and in no workflow at all
# (#128), because every one of them lived inside a step whose *name* already
# matched -- and a name is all the check above compares. The leak gate
# verify-no-tenant-origins.mjs was among them.
#
# So: every scripts/* file this script's gates invoke must appear somewhere in
# the workflow. Matching on the basename rather than the full path, because CI
# writes some of these with `./` and some without, and the question is whether
# the file is reached at all, not how it was spelled.
#
# Comment lines are stripped from the workflow before matching, and that is not
# a detail. A gate deleted from CI usually leaves its name behind in the comment
# that explained the step, so an unstripped match would report a gate as covered
# by the very comment mourning it. A check a comment can satisfy is not a check.
locally_only=(
  # Not a gate. It repairs cache symlinks whose targets Codespaces drops on
  # restart, before any gate runs; CI gets a fresh runner with nothing to repair.
  'repair-volatile-cache-links.sh'
  # Local packaging endpoint check. It needs Docker, PostgreSQL, and Temporal
  # on this machine and is not a hosted workflow step.
  'verify-packaging-endpoints.sh'
)
mapfile -t invoked < <(grep -oE '(\./)?scripts/[a-zA-Z0-9._/-]+\.(sh|mjs|rb|py)' "${BASH_SOURCE[0]}" |
  sed 's|^\./||' | sort -u)
if ((${#invoked[@]} == 0)); then
  echo "found no scripts/* invocations in $(basename "${BASH_SOURCE[0]}"); the reverse check has stopped working" >&2
  exit 1
fi
workflow_code="$(grep -vE '^[[:space:]]*#' "${workflow}")"
uncovered=()
for script in "${invoked[@]}"; do
  name="${script##*/}"
  excused=false
  for known in "${locally_only[@]}"; do
    [[ "${name}" == "${known}" ]] && excused=true
  done
  "${excused}" && continue
  grep -qF -- "${name}" <<<"${workflow_code}" || uncovered+=("${script}")
done
if ((${#uncovered[@]})); then
  printf 'this script runs gates ci.yml does not: %s\n' "$(printf '%s; ' "${uncovered[@]}")" >&2
  echo 'add them to a workflow step, or list them in locally_only with a reason.' >&2
  exit 1
fi

if ((${#failures[@]})); then
  printf 'failed: %s\n' "$(printf '%s; ' "${failures[@]}")" >&2
  exit 1
fi
[[ -n "${only}" ]] && echo "gates matching '${only}' passed" || echo 'all CI gates passed locally'

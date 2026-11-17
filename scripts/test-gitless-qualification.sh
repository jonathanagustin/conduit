#!/usr/bin/env bash
# No cluster: exercise the real event consumer/wrapper and lifecycle helper with
# known Go framework results and probes that assert their inherited context.
set -euo pipefail
repository_root="$(git rev-parse --show-toplevel)"
fixture="$(mktemp -d)"
trap 'rm -rf "${fixture}"' EXIT
mkdir -p "${fixture}/bin" "${fixture}/owner/scripts"
package=github.com/jonathanagustin/conduit/internal/gitless
jq -n --arg package "${package}" '
  ["TestAcceptanceARealSignedChannelReachesARealCluster",
   "TestAcceptanceARealSignatureIsRefusedUnderAForeignPolicy",
   "TestAcceptanceReconcilingTwiceDeploysOnce"] as $names |
  ([$names[] | {Package:$package,Action:"run",Test:.},{Package:$package,Action:"pass",Test:.}] +
  [{Package:$package,Action:"pass"}])
' >"${fixture}/valid.json"
cat >"${fixture}/bin/go" <<'SCRIPT'
#!/usr/bin/env bash
set -euo pipefail
[[ "${GITLESS_ACCEPTANCE}" == 1 && "$*" == 'test ./internal/gitless/ -run ^TestAcceptance -count=1 -json' ]] || exit 90
jq -c '.[]' "${GITLESS_EVENTS}"
exit "${GITLESS_GO_STATUS:-0}"
SCRIPT
chmod +x "${fixture}/bin/go"
run_wrapper() {
  PATH="${fixture}/bin:${PATH}" GITLESS_EVENTS="$1" GITLESS_GO_STATUS="${2:-0}" \
    "${repository_root}/scripts/test-gitless-acceptance.sh"
}
run_wrapper "${fixture}/valid.json"
for scenario in skipped missing duplicate package-only foreign-package malformed failed; do
  case "${scenario}" in
    skipped) filter='map(if .Action == "pass" and has("Test") then .Action="skip" else . end)' ;;
    missing) filter='map(select(.Test != "TestAcceptanceReconcilingTwiceDeploysOnce"))' ;;
    duplicate) filter='. + [.[0]]' ;;
    package-only) filter='map(select(has("Test")|not))' ;;
    foreign-package) filter='map(.Package="other/package")' ;;
    malformed) filter='["not an event"]' ;;
    failed) filter='map(if .Action == "pass" and has("Test") then .Action="fail" else . end)' ;;
  esac
  jq "${filter}" "${fixture}/valid.json" >"${fixture}/${scenario}.json"
  if output="$(run_wrapper "${fixture}/${scenario}.json" 2>&1)"; then echo "accepted ${scenario} acceptance bodies" >&2; exit 1; fi
  grep -qF 'every expected test must run and pass' <<<"${output}"
  echo "  refused ${scenario} acceptance bodies"
done
if run_wrapper "${fixture}/valid.json" 1 >"${fixture}/failed-go" 2>&1; then echo 'ignored go test exit' >&2; exit 1; fi
grep -qF 'gitless acceptance: FAILED' "${fixture}/failed-go"

cat >"${fixture}/bin/kubectl" <<'SCRIPT'
#!/usr/bin/env bash
exit 1
SCRIPT
chmod +x "${fixture}/bin/kubectl"
if PATH="${fixture}/bin:${PATH}" GITLESS_STRICT_ACCEPTANCE=1 \
  "${repository_root}/scripts/test-gitless-flux-manifests.sh" >"${fixture}/missing-crd" 2>&1; then
  echo 'strict Flux probe silently skipped missing CRDs' >&2; exit 1
fi
grep -qF 'required acceptance unavailable' "${fixture}/missing-crd"

# The actual shared helper runs both child probes while the owner's disposable
# context still exists. A failure propagates and prevents the next probe.
cat >"${fixture}/owner/scripts/test-gitless-flux-manifests.sh" <<'SCRIPT'
#!/usr/bin/env bash
set -euo pipefail
[[ "${GITLESS_STRICT_ACCEPTANCE}" == 1 && "${KUBECONFIG}" == "${PROBE_FIXTURE}/kubeconfig" && -s "${KUBECONFIG}" ]]
[[ "$(command -v kubectl)" == "${PROBE_FIXTURE}/bin/kubectl" ]]
echo flux >>"${PROBE_FIXTURE}/order"
[[ ! -e "${PROBE_FIXTURE}/fail-flux" ]]
SCRIPT
cat >"${fixture}/owner/scripts/test-gitless-acceptance.sh" <<'SCRIPT'
#!/usr/bin/env bash
set -euo pipefail
[[ "${KUBECONFIG}" == "${PROBE_FIXTURE}/kubeconfig" && -s "${KUBECONFIG}" ]]
[[ "$(command -v kubectl)" == "${PROBE_FIXTURE}/bin/kubectl" ]]
echo acceptance >>"${PROBE_FIXTURE}/order"
[[ ! -e "${PROBE_FIXTURE}/fail-acceptance" ]]
SCRIPT
chmod +x "${fixture}/owner/scripts/"*.sh
run_owner() (
  set -e
  trap 'echo cleanup >>"${fixture}/order"; rm -f "${fixture}/kubeconfig"' EXIT
  export PROBE_FIXTURE="${fixture}"
  printf owning-context >"${fixture}/kubeconfig"
  source "${repository_root}/scripts/lib/gitless-cluster-probes.sh"
  run_gitless_cluster_probes "${fixture}/owner" "${fixture}/bin" "${fixture}/kubeconfig"
)
run_owner
[[ "$(cat "${fixture}/order")" == $'flux\nacceptance\ncleanup' && ! -e "${fixture}/kubeconfig" ]]
for probe in flux acceptance; do
  rm "${fixture}/order"
  touch "${fixture}/fail-${probe}"
  if run_owner; then echo "ignored ${probe} failure" >&2; exit 1; fi
  [[ "$(tail -1 "${fixture}/order")" == cleanup && ! -e "${fixture}/kubeconfig" ]]
  if [[ "${probe}" == flux ]]; then ! grep -q acceptance "${fixture}/order"; fi
  rm "${fixture}/fail-${probe}"
done
echo 'Gitless strict outcomes and owning-cluster lifetime: pass'

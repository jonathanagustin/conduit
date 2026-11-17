#!/usr/bin/env bash
set -euo pipefail
repository_root="$(git rev-parse --show-toplevel)"
source "${repository_root}/scripts/testdata/cluster-qualification-fixture.sh"
fixture="$(mktemp -d)"
trap 'rm -rf "${fixture}"' EXIT
mkdir -p "${fixture}/bin"
cluster_qualification_fake_gh >"${fixture}/bin/gh"
chmod +x "${fixture}/bin/gh"
sha="$(printf 'a%.0s' {1..40})"
cluster_qualification_fixture "${fixture}/valid" "${sha}"
run_guard() {
  PATH="${fixture}/bin:${PATH}" RELEASE_FIXTURE="$1" GITHUB_API_MAX_ATTEMPTS=1 \
    GITHUB_REPOSITORY=jonathanagustin/conduit RELEASE_COMMIT="${sha}" \
    "${repository_root}/scripts/verify-cluster-qualification.sh"
}
run_guard "${fixture}/valid"
grep -qF '/attempts/2/jobs?per_page=100&page=1' "${fixture}/valid/requests"
grep -qF '?event=workflow_dispatch&head_sha=' "${fixture}/valid/requests"

refuse() {
  local name="$1" file="$2" filter="$3" expected="$4" directory="${fixture}/$1" output
  cp -R "${fixture}/valid" "${directory}"
  rm -f "${directory}/requests"
  if [[ "${file}" == marker ]]; then
    touch "${directory}/${filter}"
  else
    jq "${filter}" "${directory}/${file}" >"${directory}/changed"
    mv "${directory}/changed" "${directory}/${file}"
  fi
  if output="$(run_guard "${directory}" 2>&1)"; then echo "accepted invalid qualification: ${name}" >&2; exit 1; fi
  grep -qF "${expected}" <<<"${output}" || { echo "wrong refusal ${name}: ${output}" >&2; exit 1; }
  printf '  refused %s\n' "${name}"
}
history='no complete, valid exact-commit dispatch history'
jobs='lacks every required successful job/acceptance step'
refuse missing-run platform-e2e.yml-runs.json '.total_count=0 | .workflow_runs=[]' "${history}"
refuse wrong-sha platform-e2e.yml-runs.json '.workflow_runs[0].head_sha="bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"' "${history}"
refuse foreign-repository platform-e2e.yml-runs.json '.workflow_runs[0].repository.full_name="other/conduit"' "${history}"
refuse foreign-head platform-e2e.yml-runs.json '.workflow_runs[0].head_repository.full_name="other/conduit"' "${history}"
refuse wrong-workflow platform-e2e.yml-runs.json '.workflow_runs[0].workflow_id=99' "${history}"
refuse wrong-path platform-e2e.yml-runs.json '.workflow_runs[0].path=".github/workflows/other.yml"' "${history}"
refuse push-event platform-e2e.yml-runs.json '.workflow_runs[0].event="push"' "${history}"
refuse truncated-runs platform-e2e.yml-runs.json '.total_count=2' "${history}"
refuse excessive-history platform-e2e.yml-runs.json '.total_count=101' "${history}"
refuse invalid-attempt platform-e2e.yml-runs.json '.workflow_runs[0].run_attempt=0' "${history}"
refuse inactive-workflow platform-e2e.yml-metadata.json '.state="disabled_manually"' 'not the active expected workflow'
refuse run-detail-mismatch run-100.json '.head_sha="bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"' 'run changed while being qualified'
refuse missing-matrix-job jobs-100-2.json '.jobs|=.[0:1] | .total_count=1' "${jobs}"
refuse duplicate-matrix-job jobs-100-2.json '.jobs[1]=.jobs[0]' "${jobs}"
refuse truncated-job-page jobs-100-2.json '.jobs|=.[0:1]' "${jobs}"
refuse failed-job jobs-100-2.json '.jobs[0].conclusion="failure"' "${jobs}"
refuse skipped-job jobs-100-2.json '.jobs[0].conclusion="skipped"' "${jobs}"
refuse wrong-job-run jobs-100-2.json '.jobs[0].run_id=99' "${jobs}"
refuse wrong-job-repo jobs-100-2.json '.jobs[0].run_url="https://api.github.com/repos/other/conduit/actions/runs/100"' "${jobs}"
refuse wrong-job-sha jobs-100-2.json '.jobs[0].head_sha="bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"' "${jobs}"
refuse wrong-job-attempt jobs-100-2.json '.jobs[0].run_attempt=1' "${jobs}"
refuse skipped-acceptance-step jobs-100-2.json '.jobs[0].steps[0].conclusion="skipped"' "${jobs}"
refuse missing-acceptance-step jobs-100-2.json '.jobs[0].steps=[]' "${jobs}"
refuse failed-gitless-job jobs-200-2.json '.jobs[0].conclusion="failure"' "${jobs}"
refuse missing-registry-proof jobs-200-2.json '.jobs[0].steps|=.[0:1]' "${jobs}"
refuse api-error marker api-error 'cannot read platform-e2e.yml metadata'
refuse malformed-json marker malformed-json 'not the active expected workflow'
refuse rerun-race marker change-attempt 'run changed while checking jobs'
refuse dispatch-race marker new-dispatch 'has a newer or changed dispatch'

# Older green evidence must not hide the newer failed/in-progress attempt.
for status in failure in_progress cancelled; do
  directory="${fixture}/newer-${status}"
  cp -R "${fixture}/valid" "${directory}"
  rm -f "${directory}/requests"
  jq --arg status "${status}" '.id=101 | .run_number=8 | if $status == "in_progress" then .status=$status | .conclusion=null else .conclusion=$status end' \
    "${directory}/run-100.json" >"${directory}/run-101.json"
  jq --slurpfile newer "${directory}/run-101.json" '.total_count=2 | .workflow_runs += $newer' \
    "${directory}/platform-e2e.yml-runs.json" >"${directory}/changed"
  mv "${directory}/changed" "${directory}/platform-e2e.yml-runs.json"
  if output="$(run_guard "${directory}" 2>&1)"; then echo "accepted newer ${status}" >&2; exit 1; fi
  grep -qF 'not completed/success' <<<"${output}"
  echo "  refused newer-${status}"
done
printf 'cluster qualification API guards: pass\n'

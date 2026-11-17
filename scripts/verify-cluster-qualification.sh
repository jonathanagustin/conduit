#!/usr/bin/env bash
# Read-only release qualification. Trunk's squash-head bridge is deliberately
# absent: a cluster run must have exercised the exact commit being published.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
repository="${GITHUB_REPOSITORY:-}"
commit="${RELEASE_COMMIT:-${GITHUB_SHA:-}}"
contract="${repo_root}/scripts/cluster-qualification.json"
fail() { echo "cluster qualification: $*" >&2; exit 1; }
[[ "${repository}" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]] || fail 'GITHUB_REPOSITORY must name owner/repository'
[[ "${commit}" =~ ^[0-9a-f]{40}$ ]] || fail 'release commit must be a full 40-character SHA'
api() { "${repo_root}/scripts/retry-github-api.sh" gh api "$1"; }
specifications="$(jq -ce '
  select((.workflows | map(.file) | sort) == ["platform-e2e.yml", "platform-gitless-e2e.yml"]) |
  .workflows[] | select((.jobs | length) > 0 and (.steps | length) > 0)
' "${contract}")" || fail 'invalid qualification contract'
[[ "$(wc -l <<<"${specifications}" | tr -d " ")" == 2 ]] || fail 'incomplete qualification contract'

# Bounded, complete result sets, never a truncated first page or a success-only
# search that could hide a newer failed/running run. Operators exceeding this
# bound must resolve the history rather than silently qualify older evidence.
latest_run() {
  local workflow="$1" workflow_id="$2" response
  response="$(api "repos/${repository}/actions/workflows/${workflow}/runs?event=workflow_dispatch&head_sha=${commit}&per_page=100&page=1")" || fail "cannot read ${workflow} runs"
  jq -ce --arg repo "${repository}" --arg sha "${commit}" --arg path ".github/workflows/${workflow}" --argjson workflow_id "${workflow_id}" '
    def integer: type == "number" and . == floor and . > 0;
    select(.total_count | integer) |
    select(.total_count <= 100 and (.workflow_runs | type) == "array" and .total_count == (.workflow_runs | length)) |
    select(all(.workflow_runs[];
      (.id | integer) and (.run_number | integer) and (.run_attempt | integer) and
      .workflow_id == $workflow_id and (.path | split("@")[0]) == $path and
      .event == "workflow_dispatch" and .head_sha == $sha and
      .repository.full_name == $repo and .head_repository.full_name == $repo)) |
    select(([.workflow_runs[].id] | unique | length) == .total_count) |
    select(([.workflow_runs[].run_number] | unique | length) == .total_count) |
    .workflow_runs | max_by(.run_number)
  ' <<<"${response}" || fail "${workflow} has no complete, valid exact-commit dispatch history (maximum 100 runs)"
}

while IFS= read -r specification; do
  workflow="$(jq -er .file <<<"${specification}")"
  metadata="$(api "repos/${repository}/actions/workflows/${workflow}")" || fail "cannot read ${workflow} metadata"
  workflow_id="$(jq -er --arg path ".github/workflows/${workflow}" '
    select(.path == $path and .state == "active") | .id |
    select(type == "number" and . == floor and . > 0)
  ' <<<"${metadata}")" || fail "${workflow} is not the active expected workflow"
  selected="$(latest_run "${workflow}" "${workflow_id}")"
  run_id="$(jq -er .id <<<"${selected}")"
  attempt="$(jq -er .run_attempt <<<"${selected}")"
  detail="$(api "repos/${repository}/actions/runs/${run_id}")" || fail "cannot read ${workflow} run ${run_id}"
  # Check independently read detail, not just the list's summary. An in-progress
  # rerun invalidates the old green attempt, even if its previous jobs remain.
  identity='[.id,.workflow_id,.path,.event,.head_sha,.repository.full_name,.head_repository.full_name,.run_number,.run_attempt,.status,.conclusion]'
  [[ "$(jq -c "${identity}" <<<"${selected}")" == "$(jq -c "${identity}" <<<"${detail}")" ]] || fail "${workflow} run changed while being qualified"
  jq -e '.status == "completed" and .conclusion == "success"' <<<"${detail}" >/dev/null ||
    fail "${workflow} latest exact-commit dispatch is not completed/success (status=$(jq -r .status <<<"${detail}"), conclusion=$(jq -r .conclusion <<<"${detail}"))"

  jobs="$(api "repos/${repository}/actions/runs/${run_id}/attempts/${attempt}/jobs?per_page=100&page=1")" || fail "cannot read ${workflow} attempt ${attempt} jobs"
  # GitHub's job object does not promise run_attempt. The attempt-specific URL
  # above and the stable run read below bind jobs to the actual latest attempt.
  if ! jq -e --arg repo "${repository}" --arg sha "${commit}" --argjson run_id "${run_id}" --argjson attempt "${attempt}" --argjson spec "${specification}" '
    .total_count == ($spec.jobs | length) and (.jobs | type) == "array" and
    (.jobs | length) == .total_count and
    ([.jobs[].id] | unique | length) == .total_count and
    ([.jobs[].name] | sort) == ($spec.jobs | sort) and
    all(.jobs[];
      (.id | type == "number" and . == floor and . > 0) and
      .run_id == $run_id and .run_url == ("https://api.github.com/repos/" + $repo + "/actions/runs/" + ($run_id | tostring)) and
      ((has("run_attempt") | not) or .run_attempt == $attempt) and
      .head_sha == $sha and .status == "completed" and .conclusion == "success" and
      (.steps | type) == "array" and
      ([.steps[] | select(.name as $name | $spec.steps | index($name)) | .name] | sort) == ($spec.steps | sort) and
      all(.steps[] | select(.name as $name | $spec.steps | index($name)); .status == "completed" and .conclusion == "success"))
  ' <<<"${jobs}" >/dev/null; then
    jq -r --argjson spec "${specification}" '
      .jobs[]? | "job \(.name): status=\(.status) conclusion=\(.conclusion)",
      (.steps[]? | select(.name as $name | $spec.steps | index($name)) |
        "  acceptance step \(.name): status=\(.status) conclusion=\(.conclusion)")
    ' <<<"${jobs}" >&2 || true
    fail "${workflow} attempt ${attempt} lacks every required successful job/acceptance step"
  fi
  after="$(api "repos/${repository}/actions/runs/${run_id}")" || fail "cannot recheck ${workflow} run"
  [[ "$(jq -c "${identity}" <<<"${detail}")" == "$(jq -c "${identity}" <<<"${after}")" ]] || fail "${workflow} run changed while checking jobs"
  latest="$(latest_run "${workflow}" "${workflow_id}")"
  [[ "$(jq -c "${identity}" <<<"${detail}")" == "$(jq -c "${identity}" <<<"${latest}")" ]] || fail "${workflow} has a newer or changed dispatch"
  printf 'cluster qualification: %s sha=%s run=%s attempt=%s https://github.com/%s/actions/runs/%s/attempts/%s\n' \
    "${workflow}" "${commit}" "${run_id}" "${attempt}" "${repository}" "${run_id}" "${attempt}"
done <<<"${specifications}"

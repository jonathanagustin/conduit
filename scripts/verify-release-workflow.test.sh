#!/usr/bin/env bash
# Proves the release path's three guards refuse what they exist to refuse.
#
# The guards cannot be exercised by cutting a release -- that would tag the real
# repository and publish -- so each one is run here directly against a mutated
# input: a version that disagrees with VERSION, a commit whose Trunk gate is not
# green, a publication dispatched from a branch, and a workflow file edited to
# drop each guarantee. A gate nobody has watched refuse anything is a comment
# with a shebang.
set -euo pipefail

repository_root="$(git rev-parse --show-toplevel)"
source "${repository_root}/scripts/testdata/cluster-qualification-fixture.sh"
preconditions="${repository_root}/scripts/verify-release-preconditions.sh"
publication_ref="${repository_root}/scripts/verify-publication-ref.sh"
verifier="${repository_root}/scripts/verify-release-workflow.rb"
version="$(tr -d '[:space:]' < "${repository_root}/VERSION")"
commit="$(printf 'a%.0s' {1..40})"
head_commit="$(printf 'b%.0s' {1..40})"

fixture="$(mktemp -d /tmp/platform-release-workflow-test.XXXXXXXX)"
cleanup() {
  case "${fixture}" in
    /tmp/platform-release-workflow-test.*) rm -rf -- "${fixture}" ;;
  esac
}
trap cleanup EXIT

# --- a GitHub that answers from files -----------------------------------
# The precondition script reads everything about the release through `gh api`,
# which is what makes it testable at all: a scenario is four small JSON files.
mkdir -p "${fixture}/bin"
cluster_qualification_fake_gh >"${fixture}/bin/gh"
chmod 0755 "${fixture}/bin/gh"

scenario() {
  local name="$1" tags="$2" compare="$3" commit_status="$4" pulls="$5" head_status="$6"
  local directory="${fixture}/${name}"
  mkdir -p "${directory}"
  cluster_qualification_fixture "${directory}" "${commit}"
  printf '%s\n' "${tags}" > "${directory}/tags.json"
  printf '%s\n' "${compare}" > "${directory}/compare.json"
  printf '%s\n' "${commit_status}" > "${directory}/status-${commit}.json"
  printf '%s\n' "${pulls}" > "${directory}/pulls-${commit}.json"
  printf '%s\n' "${head_status}" > "${directory}/status-${head_commit}.json"
  printf '%s' "${directory}"
}

run_preconditions() {
  local directory="$1" release_version="$2"
  PATH="${fixture}/bin:${PATH}" \
    RELEASE_FIXTURE="${directory}" \
    GITHUB_API_MAX_ATTEMPTS=1 \
    GITHUB_API_INITIAL_DELAY_SECONDS=0 \
    GITHUB_REPOSITORY=jonathanagustin/conduit \
    RELEASE_COMMIT="${commit}" \
    RELEASE_VERSION="${release_version}" \
    "${preconditions}"
}

expect_preconditions_refusal() {
  local name="$1" directory="$2" release_version="$3" expected="$4"
  local output
  if output="$(run_preconditions "${directory}" "${release_version}" 2>&1)"; then
    printf 'expected the release preconditions to refuse: %s\n' "${name}" >&2
    printf '%s\n' "${output}" >&2
    exit 1
  fi
  grep -qF "${expected}" <<<"${output}" || {
    printf 'the refusal for %s does not explain itself: %s\n' "${name}" "${output}" >&2
    exit 1
  }
  printf '  refused %-28s %s\n' "${name}" "$(head -n1 <<<"${output}")"
}

green='{"statuses":[{"context":"Trunk gate","state":"success"}]}'
red='{"statuses":[{"context":"Trunk gate","state":"failure"}]}'
pending='{"statuses":[{"context":"Trunk gate","state":"pending"}]}'
unverified='{"statuses":[]}'
no_pulls='[]'
squashed="[{\"merged_at\":\"2026-08-18T00:00:00Z\",\"merge_commit_sha\":\"${commit}\",\"head\":{\"sha\":\"${head_commit}\"}}]"

printf 'release preconditions\n'

# The ordinary case, and the shape it actually takes here: pull requests are
# squash-merged, so the commit on main carries no status of its own and the
# gate's verdict lives on the head it authenticated.
direct="$(scenario direct '[]' '{"status":"behind"}' "${green}" "${no_pulls}" "${unverified}")"
run_preconditions "${direct}" "${version}" | sed 's/^/  /'
bridged="$(scenario bridged '[]' '{"status":"behind"}' "${unverified}" "${squashed}" "${green}")"
run_preconditions "${bridged}" "${version}" | sed 's/^/  /'

# The three refusals #196 asks for, plus the ones that fall out of the same
# reads while they are already being made.
disagreeing="$(scenario disagreeing '[]' '{"status":"behind"}' "${green}" "${no_pulls}" "${unverified}")"
expect_preconditions_refusal 'version disagrees' "${disagreeing}" 9.9.9 \
  "The release was instructed as 9.9.9, and VERSION at ${commit} says ${version}."
expect_preconditions_refusal 'version is not SemVer' "${disagreeing}" "v${version}" \
  'three dot-separated numbers without a leading v'

failed="$(scenario failed '[]' '{"status":"behind"}' "${red}" "${no_pulls}" "${unverified}")"
expect_preconditions_refusal 'Trunk gate failed' "${failed}" "${version}" \
  "The 'Trunk gate' status for ${commit} is 'failure', not 'success'."
running="$(scenario running '[]' '{"status":"behind"}' "${pending}" "${no_pulls}" "${unverified}")"
expect_preconditions_refusal 'Trunk gate still running' "${running}" "${version}" \
  "is 'pending', not 'success'"
never="$(scenario never '[]' '{"status":"behind"}' "${unverified}" "${no_pulls}" "${unverified}")"
expect_preconditions_refusal 'Trunk gate never ran' "${never}" "${version}" \
  "is 'none', not 'success'"
# A green status on an unmerged head is real, which is exactly why containment
# in main is checked separately: without it, dispatching from a short-lived
# branch would tag a commit main has never seen.
unmerged="$(scenario unmerged '[]' '{"status":"diverged"}' "${green}" "${no_pulls}" "${unverified}")"
expect_preconditions_refusal 'commit is not on main' "${unmerged}" "${version}" \
  "is not contained in main"
taken="$(scenario taken "[{\"ref\":\"refs/tags/v${version}\",\"object\":{\"sha\":\"${head_commit}\"}}]" \
  '{"status":"behind"}' "${green}" "${no_pulls}" "${unverified}")"
expect_preconditions_refusal 'tag already exists' "${taken}" "${version}" \
  "v${version} already exists and points at ${head_commit}"

# --- publication may only start from a release tag ----------------------

printf 'publication ref\n'
run_publication_ref() {
  PATH="${fixture}/bin:${PATH}" RELEASE_FIXTURE="${direct}" GITHUB_API_MAX_ATTEMPTS=1 \
    GITHUB_REPOSITORY=jonathanagustin/conduit GITHUB_SHA="${commit}" \
    GITHUB_REF="$1" GITHUB_REF_NAME="$2" "${publication_ref}"
}
expect_publication_refusal() {
  local name="$1" ref="$2" ref_name="$3" expected="$4"
  local output
  if output="$(run_publication_ref "${ref}" "${ref_name}" 2>&1)"; then
    printf 'expected publication to refuse: %s\n' "${name}" >&2
    exit 1
  fi
  grep -qF "${expected}" <<<"${output}" || {
    printf 'the refusal for %s does not explain itself: %s\n' "${name}" "${output}" >&2
    exit 1
  }
  printf '  refused %-28s %s\n' "${name}" "$(head -n1 <<<"${output}")"
}

run_publication_ref "refs/tags/v${version}" "v${version}" | sed 's/^/  /'
expect_publication_refusal 'branch ref' refs/heads/main main \
  'published a fraction of a release'
expect_publication_refusal 'unversioned tag' refs/tags/nightly nightly \
  'is not a vX.Y.Z release tag'
expect_publication_refusal 'tag disagrees with VERSION' refs/tags/v9.9.9 v9.9.9 \
  "VERSION at this commit says ${version}"

# Both independently reachable entry points must consult the same exact-SHA
# qualification oracle; preserving Trunk's squash bridge must not bridge E2E.
cp "${direct}/platform-e2e.yml-runs.json" "${fixture}/saved-runs.json"
printf '{"total_count":0,"workflow_runs":[]}' >"${direct}/platform-e2e.yml-runs.json"
expect_preconditions_refusal 'cluster qualification missing' "${direct}" "${version}" \
  'no complete, valid exact-commit dispatch history'
expect_publication_refusal 'direct publication unqualified' "refs/tags/v${version}" "v${version}" \
  'no complete, valid exact-commit dispatch history'
cp "${fixture}/saved-runs.json" "${direct}/platform-e2e.yml-runs.json"

"${repository_root}/scripts/verify-cluster-qualification.test.sh"
"${repository_root}/scripts/test-gitless-qualification.sh"

# --- the workflow files keep the shape the guards assume ----------------

printf 'release workflow contract\n'
workflows="${repository_root}/.github/workflows"
expect_workflow_refusal() {
  local name="$1"
  shift
  local expected=""
  if [[ $# -ge 2 && "$1" == --reason ]]; then
    expected="$2"
    shift 2
  fi
  local directory="${fixture}/workflow-${name}"
  mkdir -p "${directory}"
  cp "${workflows}/platform-release.yml" "${workflows}/platform-images.yml" \
    "${workflows}/platform-ci.yml" "${workflows}/platform-e2e.yml" \
    "${workflows}/platform-gitless-e2e.yml" "${directory}/"
  "$@" "${directory}"
  local output
  if output="$(ruby "${verifier}" "${directory}/platform-release.yml" \
    "${directory}/platform-images.yml" "${directory}/platform-ci.yml" \
    "${directory}/platform-e2e.yml" "${directory}/platform-gitless-e2e.yml" 2>&1)"; then
    printf 'expected the workflow mutation to fail: %s\n' "${name}" >&2
    exit 1
  fi
  if [[ -n "$expected" ]] && ! grep -qF "$expected" <<<"$output"; then
    printf 'workflow refusal for %s did not explain the targeted defect: %s\n' "$name" "$output" >&2
    exit 1
  fi
  printf '  refused %-28s %s\n' "${name}" "${output}"
}

edit_workflow() {
  ruby -ryaml -e '
    path, edit = ARGV
    document = YAML.safe_load_file(path, aliases: true)
    eval(edit)
    File.write(path, YAML.dump(document))
  ' "$1" "$2"
}

drop_trunk_activity() {
  ruby -ryaml -e '
    document = YAML.safe_load_file(ARGV[0], aliases: true)
    triggers = document["on"] || document[true]
    pull_request = triggers.fetch("pull_request")
    if ARGV[1] == "default"
      pull_request.delete("types")
    else
      pull_request.fetch("types").delete(ARGV[1])
    end
    File.write(ARGV[0], YAML.dump(document))
  ' "$2/platform-ci.yml" "$1"
}

automatic_release_trigger() {
  edit_workflow "$1/platform-release.yml" '(document["on"] || document[true])["push"] = {"tags" => ["v*"]}'
}
automatic_publication_trigger() {
  edit_workflow "$1/platform-images.yml" '(document["on"] || document[true])["workflow_run"] = {"workflows" => ["Trunk gate"]}'
}
drop_preconditions() {
  edit_workflow "$1/platform-release.yml" 'document["jobs"]["release"]["steps"].reject! { |step| step["run"].to_s.include?("verify-release-preconditions.sh") }'
}
tag_before_verifying() {
  # The steps in the other order: a refusal that arrives after the tag exists
  # refuses nothing, because the tag is what cannot be taken back.
  ruby -e '
    lines = File.readlines(ARGV[0])
    verify = lines.each_index.find { |i| lines[i].include?("Verify release preconditions") }
    tag = lines.each_index.find { |i| lines[i].include?("Create the release tag") }
    finish = lines.each_index.find { |i| i > tag && lines[i].include?("Dispatch publication") }
    reordered = lines[0...verify] + lines[tag...finish] + lines[verify...tag] + lines[finish..]
    File.write(ARGV[0], reordered.join)
  ' "$1/platform-release.yml"
}
drop_publication_ref_guard() {
  edit_workflow "$1/platform-images.yml" 'document["jobs"]["verify-publication-ref"]["steps"].reject! { |step| step["run"].to_s.include?("verify-publication-ref.sh") }'
}
detach_build_from_preflight() {
  edit_workflow "$1/platform-images.yml" 'document["jobs"].values.find { |job| job["needs"] == "verify-publication-ref" }.delete("needs")'
}
rename_trunk_status_context() {
  ruby -e 'File.write(ARGV[0], File.read(ARGV[0]).gsub("context=\x27Trunk gate\x27", "context=\x27Trunk verification\x27"))' "$1/platform-ci.yml"
}
widen_release_permissions() {
  edit_workflow "$1/platform-release.yml" 'document["jobs"]["release"]["permissions"]["packages"] = "write"'
}
drop_pull_request_permission() {
  edit_workflow "$1/platform-release.yml" 'document["jobs"]["release"]["permissions"].delete("pull-requests")'
}
skip_publication_guard() {
  edit_workflow "$1/platform-images.yml" 'document["jobs"]["verify-publication-ref"]["if"] = false'
}
ignore_spoke_failure() {
  edit_workflow "$1/platform-e2e.yml" 'document["jobs"]["spoke"]["steps"].find { |step| step["name"] == "Run Hub and Spoke acceptance" }["continue-on-error"] = true'
}
drop_helm_matrix_job() {
  edit_workflow "$1/platform-e2e.yml" 'document["jobs"]["spoke"]["strategy"]["matrix"]["helm"].pop'
}
skip_gitless_followups() {
  edit_workflow "$1/platform-gitless-e2e.yml" 'document["jobs"]["acceptance"]["steps"].find { |step| step["run"] == "./scripts/test-gitless-clean-cluster.sh" }.delete("env")'
}
ignore_gitless_registry_failure() {
  edit_workflow "$1/platform-gitless-e2e.yml" 'document["jobs"]["acceptance"]["steps"].last["continue-on-error"] = true'
}
automatic_acceptance_trigger() {
  edit_workflow "$1/platform-gitless-e2e.yml" '(document["on"] || document[true])["schedule"] = [{"cron" => "0 0 * * *"}]'
}

# A reordered block-style list is the same contract; parse YAML rather than
# depending on the workflow's current inline spelling or activity order.
trunk_positive="$fixture/workflow-trunk-reordered"
mkdir -p "$trunk_positive"
cp "$workflows/platform-release.yml" "$workflows/platform-images.yml" \
  "$workflows/platform-ci.yml" "$trunk_positive/"
ruby -ryaml -e '
  document = YAML.safe_load_file(ARGV[0], aliases: true)
  triggers = document["on"] || document[true]
  triggers.fetch("pull_request").fetch("types").reverse!
  File.write(ARGV[0], YAML.dump(document))
' "$trunk_positive/platform-ci.yml"
ruby "$verifier" "$trunk_positive/platform-release.yml" \
  "$trunk_positive/platform-images.yml" "$trunk_positive/platform-ci.yml"

trunk_activity_refusal='Trunk pull_request must explicitly cover opened, synchronize, reopened and ready_for_review'
expect_workflow_refusal missing_ready_transition --reason "$trunk_activity_refusal" drop_trunk_activity ready_for_review
expect_workflow_refusal default_pr_activities --reason "$trunk_activity_refusal" drop_trunk_activity default
expect_workflow_refusal missing_opened_activity --reason "$trunk_activity_refusal" drop_trunk_activity opened
expect_workflow_refusal missing_synchronize_activity --reason "$trunk_activity_refusal" drop_trunk_activity synchronize
expect_workflow_refusal missing_reopened_activity --reason "$trunk_activity_refusal" drop_trunk_activity reopened

expect_workflow_refusal automatic_release_trigger automatic_release_trigger
expect_workflow_refusal automatic_publication_trigger automatic_publication_trigger
expect_workflow_refusal unverified_release drop_preconditions
expect_workflow_refusal tag_before_verifying tag_before_verifying
expect_workflow_refusal unguarded_publication drop_publication_ref_guard
expect_workflow_refusal detached_publication_job detach_build_from_preflight
expect_workflow_refusal renamed_status_context rename_trunk_status_context
expect_workflow_refusal widened_release_permissions widen_release_permissions
expect_workflow_refusal missing_pull_request_permission drop_pull_request_permission
expect_workflow_refusal skipped_publication_guard --reason 'must run unconditionally and propagate failure' skip_publication_guard
expect_workflow_refusal ignored_spoke_failure --reason 'must run unconditionally and propagate failure' ignore_spoke_failure
expect_workflow_refusal missing_helm_matrix_job --reason 'Spoke qualification must cover both supported Helm matrix jobs' drop_helm_matrix_job
expect_workflow_refusal missing_gitless_followups --reason 'Gitless workflow must run strict followups inside the owning cluster lifetime' skip_gitless_followups
expect_workflow_refusal ignored_gitless_registry_failure --reason 'must run unconditionally and propagate failure' ignore_gitless_registry_failure
expect_workflow_refusal automatic_acceptance_trigger --reason 'must remain manual-only with read-only authority' automatic_acceptance_trigger

printf 'release path guards: pass\n'

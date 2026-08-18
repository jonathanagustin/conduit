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
cat > "${fixture}/bin/gh" <<'SCRIPT'
#!/usr/bin/env bash
set -uo pipefail
if [[ "${1:-}" != api ]]; then
  echo "gh: this stand-in only answers api calls, got: $*" >&2
  exit 1
fi
path="$2"
file=""
case "${path}" in
  */git/matching-refs/tags/*) file=tags.json ;;
  */compare/*) file=compare.json ;;
  */commits/*/status) sha="${path#*/commits/}"; file="status-${sha%/status}.json" ;;
  */commits/*/pulls) sha="${path#*/commits/}"; file="pulls-${sha%/pulls}.json" ;;
esac
if [[ -z "${file}" || ! -f "${RELEASE_FIXTURE}/${file}" ]]; then
  # Shaped like gh's own permanent failure so retry-github-api.sh reports it at
  # once instead of retrying an answer that is never coming.
  echo "gh: Not Found (HTTP 404) ${path}" >&2
  exit 1
fi
cat "${RELEASE_FIXTURE}/${file}"
SCRIPT
chmod 0755 "${fixture}/bin/gh"

scenario() {
  local name="$1" tags="$2" compare="$3" commit_status="$4" pulls="$5" head_status="$6"
  local directory="${fixture}/${name}"
  mkdir -p "${directory}"
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

# --- the workflow files keep the shape the guards assume ----------------

printf 'release workflow contract\n'
workflows="${repository_root}/.github/workflows"
expect_workflow_refusal() {
  local name="$1"
  shift
  local directory="${fixture}/workflow-${name}"
  mkdir -p "${directory}"
  cp "${workflows}/platform-release.yml" "${workflows}/platform-images.yml" \
    "${workflows}/platform-ci.yml" "${directory}/"
  "$@" "${directory}"
  local output
  if output="$(ruby "${verifier}" "${directory}/platform-release.yml" \
    "${directory}/platform-images.yml" "${directory}/platform-ci.yml" 2>&1)"; then
    printf 'expected the workflow mutation to fail: %s\n' "${name}" >&2
    exit 1
  fi
  printf '  refused %-28s %s\n' "${name}" "${output}"
}

automatic_release_trigger() {
  sed -i '/^  workflow_dispatch:/i\  push:\n    tags: ["v*"]' "$1/platform-release.yml"
}
automatic_publication_trigger() {
  sed -i '/^  workflow_dispatch:/i\  workflow_run:\n    workflows: [Trunk gate]' "$1/platform-images.yml"
}
drop_preconditions() {
  sed -i '/verify-release-preconditions.sh/d' "$1/platform-release.yml"
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
  sed -i '\|./scripts/verify-publication-ref.sh|d' "$1/platform-images.yml"
}
detach_build_from_preflight() {
  sed -i '0,/^    needs: verify-publication-ref$/s///' "$1/platform-images.yml"
}
rename_trunk_status_context() {
  sed -i "s/context='Trunk gate'/context='Trunk verification'/g" "$1/platform-ci.yml"
}
widen_release_permissions() {
  sed -i 's/^      statuses: read$/      statuses: read\n      packages: write/' "$1/platform-release.yml"
}
drop_pull_request_permission() {
  sed -i '/^      pull-requests: read$/d' "$1/platform-release.yml"
}

expect_workflow_refusal automatic_release_trigger automatic_release_trigger
expect_workflow_refusal automatic_publication_trigger automatic_publication_trigger
expect_workflow_refusal unverified_release drop_preconditions
expect_workflow_refusal tag_before_verifying tag_before_verifying
expect_workflow_refusal unguarded_publication drop_publication_ref_guard
expect_workflow_refusal detached_publication_job detach_build_from_preflight
expect_workflow_refusal renamed_status_context rename_trunk_status_context
expect_workflow_refusal widened_release_permissions widen_release_permissions
expect_workflow_refusal missing_pull_request_permission drop_pull_request_permission

printf 'release path guards: pass\n'

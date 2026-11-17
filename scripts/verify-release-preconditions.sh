#!/usr/bin/env bash
# Everything that must be true before a commit is allowed to become a tag.
#
# Cutting a release was four hand-coordinated steps -- bump VERSION, merge,
# create the vX.Y.Z tag by hand, dispatch publication against it -- and three of
# them were a person typing the same version string somewhere else. Nothing
# compared the four spellings. verify-version-matches-tag.sh deliberately does
# not: it fails when VERSION is behind the newest tag and passes when it is
# ahead, because the commit that bumps VERSION cannot also carry the tag that
# confirms it. So equality is never asserted on main, and the only place it was
# ever asserted was inside the publication workflow, after publication had
# already started (#196).
#
# This asserts it once, before the tag exists, which is the only moment where
# refusing is free. Everything it reads comes from the GitHub API rather than
# from the checkout, except VERSION itself, so it is checking the released
# commit as the server sees it rather than as a local clone happens to be.
#
# It does not authorize anything. A person dispatching the release workflow is
# the authorization (ADR 0001, A5); this only refuses to turn a bad instruction
# into a tag.
set -euo pipefail

fail() {
  printf '%s\n' "$*" >&2
  exit 1
}

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

version="${RELEASE_VERSION:-}"
repository="${GITHUB_REPOSITORY:-}"
commit="${RELEASE_COMMIT:-${GITHUB_SHA:-}}"
base_branch="${RELEASE_BASE_BRANCH:-main}"
# The commit-status context platform-ci.yml publishes its verdict under.
# verify-release-workflow.rb asserts the two spellings still agree: a context
# renamed on one side only would read here as "no gate has run", and the
# difference between "the gate failed" and "no gate ran" is the whole point.
trunk_gate_context='Trunk gate'

# Reads go through the retry wrapper for the same reason the Trunk gate's status
# posts do: GitHub answers a small fraction of calls with a 5xx, and a release
# that fails because one read blipped teaches people to re-dispatch reflexively,
# which is the habit this gate least wants.
api() {
  "${repo_root}/scripts/retry-github-api.sh" gh api "$@"
}

[[ -n "${version}" ]] || fail 'RELEASE_VERSION is empty; pass the version being released, without the leading v.'
[[ -n "${repository}" ]] || fail 'GITHUB_REPOSITORY is empty; this must run with the GitHub Actions environment.'

# Strict three-part SemVer, no prerelease and no build metadata. Not pedantry:
# deploy/flux/source.example.yaml trusts publications signed by
# platform-images.yml at `refs/tags/v[0-9]+\.[0-9]+\.[0-9]+` only, and
# scripts/verify-flux-runtime.rb holds it to that. A v1.0.0-rc1 tag would
# publish artifacts that every production cluster then refuses to verify, and it
# would refuse them at reconcile time rather than here.
if [[ ! "${version}" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]; then
  fail "the release version must be three dot-separated numbers without a leading v; got '${version}'."
fi
if [[ ! "${commit}" =~ ^[0-9a-f]{40}$ ]]; then
  fail "the release commit must be a full 40-character SHA; got '${commit}'."
fi

tag="v${version}"

file_version="$(tr -d '[:space:]' < "${repo_root}/VERSION")"
if [[ "${version}" != "${file_version}" ]]; then
  cat >&2 <<EOF
The release was instructed as ${version}, and VERSION at ${commit} says ${file_version}.

Every image, OCI artifact, chart, Zarf package and CLI archive is stamped from
VERSION, while the tag is what names the release and what Flux verifies. Tagging
this commit ${tag} would publish ${file_version}-labelled artifacts from a ${tag}
tree -- correctly signed and wrongly labelled, which is worse than unsigned,
because the provenance chain verifies.

Either release ${file_version}, or land a VERSION bump first.
EOF
  exit 1
fi

# A tag is the identity of a release, so it is created once and never moved.
# release-and-publication.md says the same thing from the other end: do not
# overwrite a published tag to simulate rollback.
#
# matching-refs answers 200 with an empty array when nothing matches, so a
# missing tag is data rather than an error status this would have to tell apart
# from a network failure. It is a prefix match -- v0.1.2 also matches v0.1.20 --
# so the exact ref is selected here rather than trusted from the query.
existing="$(api "repos/${repository}/git/matching-refs/tags/${tag}")"
existing_count="$(jq -er --arg ref "refs/tags/${tag}" '[.[] | select(.ref == $ref)] | length' <<<"${existing}")"
if (( existing_count != 0 )); then
  existing_sha="$(jq -er --arg ref "refs/tags/${tag}" '[.[] | select(.ref == $ref)] | .[0].object.sha' <<<"${existing}")"
  fail "${tag} already exists and points at ${existing_sha}. A published tag is immutable; release the next version instead."
fi

# A release tag must name a commit that is on the release branch. Without this,
# dispatching from a short-lived branch would tag an unmerged head -- and an
# unmerged head is exactly the kind of commit that *does* carry a green Trunk
# gate status, so the check below would have been satisfied by it.
comparison="$(api "repos/${repository}/compare/${base_branch}...${commit}")"
comparison_status="$(jq -er '.status' <<<"${comparison}")"
case "${comparison_status}" in
  identical | behind) ;;
  *)
    fail "${commit} is not contained in ${base_branch} (git reports it '${comparison_status}'). Release a commit that has been merged."
    ;;
esac

# The Trunk gate's verdict for this exact commit.
#
# The combined-status endpoint returns the most recent status per context, so
# there is at most one entry to read. "none" is returned rather than an empty
# string so that an unverified commit reads as a state in the message below.
gate_state() {
  local sha="$1" status_json
  status_json="$(api "repos/${repository}/commits/${sha}/status")"
  jq -er --arg context "${trunk_gate_context}" \
    '[.statuses[] | select(.context == $context)] | if length == 0 then "none" else .[0].state end' \
    <<<"${status_json}"
}

state="$(gate_state "${commit}")"
verified_commit="${commit}"
route='its own commit status'

# Pull requests are squash-merged here, so the commit on main is a new object
# that no gate has ever seen: the Trunk gate published its verdict against the
# pull-request head it authenticated (platform-ci.yml requires head_sha ==
# GITHUB_SHA before running anything). That head is the only place the evidence
# exists, so this follows the merge back to it -- and only accepts it when
# exactly one merged pull request names this commit as its merge commit, which
# keeps the evidence bound to this commit rather than to any pull request that
# happened to touch it.
if [[ "${state}" != success ]]; then
  merged="$(api "repos/${repository}/commits/${commit}/pulls")"
  merged_head="$(jq -er --arg commit "${commit}" \
    '[.[] | select(.merged_at != null and .merge_commit_sha == $commit)] | if length == 1 then .[0].head.sha else "" end' \
    <<<"${merged}")"
  if [[ -n "${merged_head}" ]]; then
    state="$(gate_state "${merged_head}")"
    verified_commit="${merged_head}"
    route='the head of the pull request it was squashed from'
  fi
fi

if [[ "${state}" != success ]]; then
  cat >&2 <<EOF
The '${trunk_gate_context}' status for ${commit} is '${state}', not 'success'.

Publication is a deliberate act, and it is deliberate about a verified tree. A
green gate never authorizes a release on its own (A5), but a release with no
green gate is a decision made without the evidence it claims to rest on.

Dispatch the Trunk gate for the pull request that produced this commit and let
it finish, then instruct the release again.
EOF
  exit 1
fi

RELEASE_COMMIT="${commit}" "${repo_root}/scripts/verify-cluster-qualification.sh"

cat <<EOF
release preconditions: ${tag} is unused, VERSION at ${commit} says ${version},
${commit} is contained in ${base_branch}, and the ${trunk_gate_context} status is
success via ${route} (${verified_commit}).
EOF

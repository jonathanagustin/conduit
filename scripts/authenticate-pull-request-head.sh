#!/usr/bin/env bash
# Resolves the pull request a Trunk gate run is verifying, and proves that the
# tree the run checked out is that pull request's head. Prints the pull request
# number on stdout; everything it establishes goes to stderr.
#
# This is what stops the gate verifying a tree nobody proposed, so it runs
# before any lane and every lane `needs` the job that runs it. It lives in a
# script rather than inline in the workflow because the workflow now starts two
# ways -- a `pull_request` event and a `workflow_dispatch` -- which name the same
# pull request through different variables, and a check with two paths needs a
# test that exercises both. `authenticate-pull-request-head.test.sh` is that
# test.
#
# The two paths differ only in where the identities come from:
#
#   - `pull_request`: GitHub supplies the number in the event and checks the run
#     out at `refs/pull/<number>/merge`, a commit built from the head and the
#     base that nobody proposed and that disappears. So the run pins its
#     checkout to the head sha from the event, and this proves that sha really
#     is the head of that open pull request.
#   - `workflow_dispatch`: a person names both the ref and the number, and
#     nothing relates them. So this proves the dispatched ref is the pull
#     request's branch and sits exactly on its head commit -- the original check,
#     unchanged in substance.
set -euo pipefail

fail() {
  printf 'authenticate-pull-request-head: %s\n' "$*" >&2
  exit 1
}

event="${GITHUB_EVENT_NAME:-}"
repository="${GITHUB_REPOSITORY:-}"
ref="${GITHUB_REF:-}"
# The commit every lane checks out and every Trunk status is published against.
# It is an input rather than something derived here because the workflow has to
# name it before this runs -- the checkout that puts this script on disk already
# used it -- so the only useful thing to do with it is authenticate it.
gate_sha="${GATE_SHA:-}"

[[ -n "${repository}" ]] || fail 'GITHUB_REPOSITORY is empty; this must run in GitHub Actions'
[[ -n "${gate_sha}" ]] || fail 'GATE_SHA is empty; the workflow must name the commit it verifies'

case "${event}" in
  pull_request)
    pull_request="${EVENT_PULL_REQUEST:-}"
    [[ -n "${pull_request}" ]] ||
      fail 'the pull_request event carried no pull request number'
    ;;
  workflow_dispatch)
    pull_request="${INPUT_PULL_REQUEST:-}"
    # The trigger no longer marks this input required, because the workflow can
    # now start without it from a pull_request event. It is still required here:
    # a dispatch that names no pull request would otherwise verify whatever ref
    # was dispatched and publish a Trunk status for it.
    [[ -n "${pull_request}" ]] ||
      fail 'dispatch the Trunk gate with the pull_request input set to the open pull request number'
    ;;
  *)
    # Fail closed. A trigger added without extending this would otherwise reach
    # the lanes with no identity established at all.
    fail "the Trunk gate cannot authenticate a '${event:-<unset>}' event; it runs on pull_request and workflow_dispatch"
    ;;
esac

[[ "${pull_request}" =~ ^[1-9][0-9]*$ ]] ||
  fail "pull_request must be an open pull request number, got '${pull_request}'"

# Split on tabs, not on the default whitespace: a field the API answered empty
# -- a head whose fork repository was deleted, say -- would otherwise collapse
# and shift every field after it onto the wrong variable, which reads as a
# mismatch in some unrelated check rather than as the missing field it is.
IFS=$'\t' read -r state head_sha head_ref head_repository < <(
  gh api "repos/${repository}/pulls/${pull_request}" \
    --jq '[.state, .head.sha, .head.ref, .head.repo.full_name] | @tsv'
) || fail "could not read pull request ${pull_request} from the GitHub API"

[[ "${state}" == open ]] ||
  fail "pull request ${pull_request} is '${state:-<unknown>}', not open"
# A fork head is not a tree this repository proposed, and the token on that path
# cannot write the commit status the gate reports through, so the run would look
# green while publishing nothing. Refusing is the visible failure.
[[ "${head_repository}" == "${repository}" ]] ||
  fail "pull request ${pull_request} is headed by ${head_repository:-<unknown>}, not ${repository}"
[[ "${head_sha}" == "${gate_sha}" ]] ||
  fail "pull request ${pull_request} is headed by ${head_sha}, but this run verifies ${gate_sha}"

case "${event}" in
  pull_request)
    # Ties the event this run is acting on to the pull request the API just
    # answered about: the merge ref names its own pull request number.
    [[ "${ref}" == "refs/pull/${pull_request}/merge" || "${ref}" == "refs/pull/${pull_request}/head" ]] ||
      fail "the pull_request event ref ${ref:-<unset>} does not belong to pull request ${pull_request}"
    ;;
  workflow_dispatch)
    [[ "refs/heads/${head_ref}" == "${ref}" ]] ||
      fail 'dispatch the Trunk gate from the pull request branch'
    ;;
esac

# The checkout is pinned to the same sha by a workflow expression. An expression
# that resolved to nothing would leave `ref:` empty, and a checkout with an empty
# ref silently falls back to the event's own ref -- the merge commit on the
# pull_request path. That is a tree nobody proposed, verified under a status
# published about a different commit, and nothing else here would notice.
checked_out="$(git rev-parse HEAD)"
[[ "${checked_out}" == "${gate_sha}" ]] ||
  fail "the checkout is at ${checked_out}, not the authenticated head ${gate_sha}"

printf 'authenticated pull request %s at %s (%s)\n' \
  "${pull_request}" "${gate_sha}" "${event}" >&2
printf '%s\n' "${pull_request}"

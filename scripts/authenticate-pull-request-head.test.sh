#!/usr/bin/env bash
# Exercises scripts/authenticate-pull-request-head.sh on both paths the Trunk
# gate can start from.
#
# The authenticator is the whole reason the gate cannot verify a tree nobody
# proposed, and its failure mode is silent: a run that authenticates nothing
# looks exactly like a run that authenticates everything, because both go green.
# So every refusal below is checked by its message as well as by its exit status
# -- a check that refuses for the wrong reason is a check that will stop
# refusing when the reason changes.
#
# The GitHub call is stubbed, so this runs offline and is part of the local gate
# set. The stub also records the path it was asked for, so "authenticated" means
# the script asked about the pull request it claims to have authenticated, not
# merely that it printed a number.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
script="$root/scripts/authenticate-pull-request-head.sh"
cd "$root"

stub_root="$(mktemp -d)"
trap 'rm -rf "$stub_root"' EXIT
cat >"$stub_root/gh" <<'STUB'
#!/usr/bin/env bash
# Stands in for the GitHub API so the authenticator can be exercised offline.
printf '%s\n' "$*" >>"${GH_STUB_LOG}"
printf '%s\n' "${GH_STUB_ANSWER}"
STUB
chmod 0755 "$stub_root/gh"
log="$stub_root/requests"
: >"$log"

# The checkout the authenticator sees. In the workflow this is the pull-request
# head, pinned by the `ref:` on every checkout step; here it is whatever this
# repository is checked out at, which is the same relationship.
checked_out="$(git rev-parse HEAD)"

# state, head sha, head branch, head repository -- the four fields the script
# reads out of the pull request, tab separated exactly as `@tsv` emits them.
answer() {
  printf '%s\t%s\t%s\t%s' "$1" "$2" "$3" "$4"
}
open_head="$(answer open "$checked_out" claude/fix-195-automatic-gate-trigger owner/repo)"

# event name, ref, event pull-request number, dispatch input, gate sha, API answer
authenticate() {
  : >"$log"
  env PATH="$stub_root:$PATH" \
    GH_STUB_LOG="$log" \
    GH_STUB_ANSWER="$6" \
    GITHUB_REPOSITORY=owner/repo \
    GITHUB_EVENT_NAME="$1" \
    GITHUB_REF="$2" \
    EVENT_PULL_REQUEST="$3" \
    INPUT_PULL_REQUEST="$4" \
    GATE_SHA="$5" \
    "$script"
}

expect_authenticated() {
  local name="$1" expected="$2" actual
  shift 2
  actual="$(authenticate "$@" 2>"$stub_root/stderr")" || {
    printf 'authentication of %s failed, expected pull request %s:\n' "$name" "$expected" >&2
    cat "$stub_root/stderr" >&2
    exit 1
  }
  [[ "$actual" == "$expected" ]] || {
    printf 'authentication of %s resolved pull request %s, expected %s\n' "$name" "$actual" "$expected" >&2
    exit 1
  }
  grep -Fq "repos/owner/repo/pulls/${expected}" "$log" || {
    printf 'authentication of %s did not ask GitHub about pull request %s\n' "$name" "$expected" >&2
    exit 1
  }
}

expect_refused() {
  local name="$1" reason="$2"
  shift 2
  if authenticate "$@" >/dev/null 2>"$stub_root/stderr"; then
    printf 'expected authentication to refuse %s, but it succeeded\n' "$name" >&2
    exit 1
  fi
  grep -Fq -- "$reason" "$stub_root/stderr" || {
    printf 'authentication refused %s for the wrong reason; expected %s, got:\n' "$name" "$reason" >&2
    cat "$stub_root/stderr" >&2
    exit 1
  }
}

# --- the pull_request path ----------------------------------------------
# The identities come from the event, and the run is checked out at the head sha
# the event carried rather than at the merge commit GITHUB_REF names.

expect_authenticated pull_request_event 195 \
  pull_request refs/pull/195/merge 195 '' "$checked_out" "$open_head"

# The one that matters most: the head sha is what the lanes verify and what the
# Trunk status is published against, so a run whose checkout is not the head is
# a verdict about one tree written against another.
expect_refused pull_request_head_moved \
  'but this run verifies' \
  pull_request refs/pull/195/merge 195 '' "$checked_out" \
  "$(answer open 1111111111111111111111111111111111111111 claude/fix-195-automatic-gate-trigger owner/repo)"

# A number from the event that does not match the ref the event checked out
# means the two came from different pull requests.
expect_refused pull_request_ref_of_another_pull_request \
  'does not belong to pull request 195' \
  pull_request refs/pull/42/merge 195 '' "$checked_out" "$open_head"

expect_refused pull_request_without_a_number \
  'carried no pull request number' \
  pull_request refs/pull/195/merge '' '' "$checked_out" "$open_head"

expect_refused pull_request_closed \
  "is 'closed', not open" \
  pull_request refs/pull/195/merge 195 '' "$checked_out" \
  "$(answer closed "$checked_out" claude/fix-195-automatic-gate-trigger owner/repo)"

# The pull_request path made the API read racy in a way the dispatch path never
# was: the event fires seconds after the pull request is opened, and GitHub
# answered a ten-second-old one with truncated JSON. That reached the field
# checks as empty strings, so the run failed citing some unrelated mismatch
# instead of the read that did not happen.
expect_refused pull_request_with_a_truncated_api_answer \
  'no state or head commit' \
  pull_request refs/pull/195/merge 195 '' "$checked_out" ''

expect_refused pull_request_from_a_fork \
  'is headed by someone/conduit, not owner/repo' \
  pull_request refs/pull/195/merge 195 '' "$checked_out" \
  "$(answer open "$checked_out" claude/fix-195-automatic-gate-trigger someone/conduit)"

# --- the workflow_dispatch path -----------------------------------------
# A person names the ref and the number separately and nothing relates them, so
# both have to be proven against the pull request.

expect_authenticated dispatch_from_the_head 195 \
  workflow_dispatch refs/heads/claude/fix-195-automatic-gate-trigger '' 195 "$checked_out" "$open_head"

expect_refused dispatch_from_a_stale_commit \
  'but this run verifies' \
  workflow_dispatch refs/heads/claude/fix-195-automatic-gate-trigger '' 195 "$checked_out" \
  "$(answer open 1111111111111111111111111111111111111111 claude/fix-195-automatic-gate-trigger owner/repo)"

expect_refused dispatch_from_main \
  'dispatch the Trunk gate from the pull request branch' \
  workflow_dispatch refs/heads/main '' 195 "$checked_out" "$open_head"

expect_refused dispatch_without_a_number \
  'dispatch the Trunk gate with the pull_request input set' \
  workflow_dispatch refs/heads/claude/fix-195-automatic-gate-trigger '' '' "$checked_out" "$open_head"

expect_refused dispatch_with_a_branch_name_for_a_number \
  'must be an open pull request number' \
  workflow_dispatch refs/heads/claude/fix-195-automatic-gate-trigger '' main "$checked_out" "$open_head"

# --- anything else -------------------------------------------------------
# A trigger added to the workflow without extending the authenticator must reach
# no lane at all. This is the failure that would not announce itself: the added
# trigger would simply work, with no identity established.

expect_refused a_push_event \
  "cannot authenticate a 'push' event" \
  push refs/heads/main '' '' "$checked_out" "$open_head"

expect_refused an_unnamed_checkout \
  'GATE_SHA is empty' \
  pull_request refs/pull/195/merge 195 '' '' "$open_head"

# The workflow pins every checkout to the head sha through an expression. An
# expression that resolved to nothing leaves `ref:` empty, and an empty ref is
# not an error -- the checkout quietly takes the event's own ref, which on the
# pull_request path is the merge commit. Everything above would still pass: the
# sha the run names really is the head; it is just not the tree on disk.
expect_refused an_unpinned_checkout \
  'the checkout is at' \
  pull_request refs/pull/195/merge 195 '' 2222222222222222222222222222222222222222 \
  "$(answer open 2222222222222222222222222222222222222222 claude/fix-195-automatic-gate-trigger owner/repo)"

echo 'pull-request head authentication tests passed'

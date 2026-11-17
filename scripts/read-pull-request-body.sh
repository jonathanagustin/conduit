#!/usr/bin/env bash
# Reads a pull request's body from the GitHub API and prints it on stdout, so
# that `verify-pr-fidelity.sh` gets either the body or nothing at all.
#
# It exists because the workflow used to do this inline, in a pipe:
#
#     gh api "repos/${GITHUB_REPOSITORY}/pulls/${N}" --jq .body |
#       ./scripts/verify-pr-fidelity.sh
#
# and that line had three compounding faults. The read was unretried, on the
# same API and in the same job where #246 measured GitHub answering a
# seconds-old pull request with truncated JSON. Under `bash -e` without
# `pipefail` a pipeline's status is its last command's, so a failed `gh`
# contributed an empty stdin and nothing else. And the verifier, handed an empty
# body, refused it with "pull request section ## Outcome is missing or empty" --
# a message that names a defect in a body which is, in fact, correctly filled
# in, and sends the author to edit a description that was never read.
#
# That last part is the reason this is worth a script. A gate that refuses
# correct work with a confident wrong explanation is worse than one that errors,
# because the author learns the gate produces false refusals, which is how a
# gate stops being read.
#
# So the two failures are told apart here, before the verifier ever sees the
# text, and each is reported as itself:
#
#   - the API did not answer -> a failed read, named as a failed read;
#   - the API answered with an empty body -> an empty body, which is still a
#     refusal, because a pull request with no description cannot carry the
#     fidelity record the gate is checking for.
#
# Both exit non-zero. The distinction is in the message, which is where it
# matters: one sends a person to re-run the gate, the other to write a body.
set -euo pipefail

fail() {
  printf 'read-pull-request-body: %s\n' "$*" >&2
  exit 1
}

repository="${GITHUB_REPOSITORY:-}"
pull_request="${PULL_REQUEST_NUMBER:-}"

[[ -n "${repository}" ]] || fail 'GITHUB_REPOSITORY is empty; this must run in GitHub Actions'
# Not merely non-empty. The number reaching here comes from
# authenticate-pull-request-head.sh, and a step that failed to set it would
# otherwise send `repos/owner/repo/pulls/` to the API and read whatever that
# answers.
[[ "${pull_request}" =~ ^[1-9][0-9]*$ ]] ||
  fail "PULL_REQUEST_NUMBER must be a pull request number, got '${pull_request:-<unset>}'"

# `// ""` rather than a bare `.body` because the API answers a pull request
# opened with no description with JSON null, and jq renders that as the
# four-character string "null" -- which is not an empty body, reaches the
# verifier as prose, and is refused for having no `## Outcome` section rather
# than for being empty. Mapping it to the empty string here puts it on the
# branch below that says so.
#
# Through the retry wrapper for the reason #246 established one step earlier in
# this same job: 5xx and 429 are GitHub and waiting helps, while 401, 403 and
# 422 are the token or the request and waiting only reports the same error
# later.
#
# Captured into a variable rather than piped, which is the whole point: an
# assignment's status is the substitution's, so a failed read is a failed read
# here instead of an empty stdin somewhere downstream.
body="$(
  "$(dirname -- "${BASH_SOURCE[0]}")/retry-github-api.sh" \
    gh api "repos/${repository}/pulls/${pull_request}" --jq '.body // ""'
)" || fail "could not read pull request ${pull_request}'s body from the GitHub API; this is a failed read, not a verdict on the body"

# Release Please replaces a pull-request body larger than GitHub's 65536-byte
# issue-body limit with a pointer to `release-notes.md` on the companion
# `<headRef>--release-notes` branch in this repository. Follow that exact
# same-repository pointer so the full generated release PR body (including the
# `## Outcome` and `## Tenant fidelity` header) reaches the verifier instead of
# failing as a missing section (#844).
overflow_prefix='This release is too large to preview in the pull request body. View the full release notes here: '
if [[ "${body}" == "${overflow_prefix}"* ]]; then
  overflow_url="${body#"${overflow_prefix}"}"
  overflow_url="${overflow_url%"${overflow_url##*[![:space:]]}"}"
  expected_prefix="https://github.com/${repository}/blob/"
  expected_suffix="/release-notes.md"
  if [[ "${overflow_url}" == "${expected_prefix}"*"${expected_suffix}" ]]; then
    notes_branch="${overflow_url#"${expected_prefix}"}"
    notes_branch="${notes_branch%"${expected_suffix}"}"
    if [[ "${notes_branch}" =~ ^[A-Za-z0-9._/-]+$ && "${notes_branch}" != *..* ]]; then
      encoded_content="$(
        "$(dirname -- "${BASH_SOURCE[0]}")/retry-github-api.sh" \
          gh api "repos/${repository}/contents/release-notes.md?ref=${notes_branch}" --jq '.content // ""'
      )" || fail "could not read overflow release-notes.md on branch ${notes_branch} for pull request ${pull_request}; this is a failed read, not a verdict on the body"
      body="$(tr -d '\r\n' <<<"${encoded_content}" | base64 -d)" ||
        fail "could not decode overflow release-notes.md on branch ${notes_branch} for pull request ${pull_request}"
    fi
  fi
fi

# Whitespace-only counts as empty. A body of one newline satisfies `-n` and
# carries no more record than none at all.
[[ "${body}" =~ [^[:space:]] ]] ||
  fail "pull request ${pull_request} has an empty body; it must carry the tenant fidelity record"

printf '%s\n' "${body}"

#!/usr/bin/env bash
# Proves read-pull-request-body.sh reports a failed read as a failed read.
#
# The defect it was written against produced a *refusal*, not an error: a
# transient GitHub failure reached the fidelity verifier as empty stdin and came
# back as "pull request section ## Outcome is missing or empty" about a body
# that had one. So the assertions below are about the message as much as the
# exit status -- a version of this script that failed with the wrong
# explanation would be the bug, and would pass a test that only checked for
# non-zero.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
reader="${repo_root}/scripts/read-pull-request-body.sh"
test_dir="$(mktemp -d)"
trap 'rm -rf "${test_dir}"' EXIT

# A stand-in gh on PATH, because the reader invokes `gh` through the retry
# wrapper by name. It holds a JSON API response and runs the reader's own
# `--jq` filter over it, rather than printing a canned answer: the filter is
# under test here. `.body // ""` is what turns the JSON null the API gives a
# pull request opened with no description into an empty string, and a fake that
# ignored the filter would report that case passing while the real one still
# reached the verifier as the four characters "null".
mkdir -p "${test_dir}/bin"
cat > "${test_dir}/bin/gh" <<'SCRIPT'
#!/usr/bin/env bash
set -uo pipefail
failures=0
if [[ -f "${FAKE_GH_FAILURES}" ]]; then failures="$(cat "${FAKE_GH_FAILURES}")"; fi
attempts=0
if [[ -f "${FAKE_GH_ATTEMPTS}" ]]; then attempts="$(cat "${FAKE_GH_ATTEMPTS}")"; fi
attempts=$((attempts + 1))
printf '%s\n' "${attempts}" > "${FAKE_GH_ATTEMPTS}"
if (( attempts <= failures )); then
  printf '%s\n' "${FAKE_GH_ERROR}" >&2
  exit 1
fi
filter='.'
while (( $# )); do
  case "$1" in
    --jq) filter="$2"; shift 2 ;;
    *) shift ;;
  esac
done
jq -r "${filter}" <"${FAKE_GH_RESPONSE}"
SCRIPT
chmod 0755 "${test_dir}/bin/gh"

export PATH="${test_dir}/bin:${PATH}"
export FAKE_GH_RESPONSE="${test_dir}/response"
export FAKE_GH_FAILURES="${test_dir}/failures"
export FAKE_GH_ATTEMPTS="${test_dir}/attempts"
export GITHUB_API_INITIAL_DELAY_SECONDS=0

# The message run 49 died on and #246 measured, verbatim rather than
# paraphrased, for the reason test-retry-github-api.sh quotes it verbatim: a
# reworded copy would pass while the real one still failed.
transient='gh: No server is currently available to service your request. (HTTP 503)'
permanent='gh: Resource not accessible by integration (HTTP 403)'

# JSON response, failures-before-success, error text
arrange_response() {
  printf '%s' "$1" > "${FAKE_GH_RESPONSE}"
  printf '%s\n' "$2" > "${FAKE_GH_FAILURES}"
  export FAKE_GH_ERROR="${3:-${transient}}"
  rm -f "${FAKE_GH_ATTEMPTS}"
}

# The same, for the ordinary case where the response is a pull request with a
# body. Built with jq so a body containing quotes or newlines is encoded rather
# than pasted into JSON by hand.
arrange() {
  arrange_response "$(jq -n --arg body "$1" '{body: $body}')" "$2" "${3:-}"
}

# Runs the reader and captures stdout, stderr and status separately, because
# every case below is an assertion about which of the three carries what.
run_reader() {
  set +e
  stdout="$(GITHUB_REPOSITORY=owner/repo PULL_REQUEST_NUMBER="${1:-7}" \
    "${reader}" 2>"${test_dir}/stderr")"
  status=$?
  set -e
  stderr="$(cat "${test_dir}/stderr")"
}

body='## Outcome

It works.

### Deliberate divergences

None.'

# A body that reads first time is passed through unchanged. This is the case
# every other assertion is a deviation from, so it is worth checking the text
# survives rather than only that the exit status is zero: a reader that dropped
# the body would make every pull request fail the sections check for real.
arrange "${body}" 0
run_reader
test "${status}" = 0
test "${stdout}" = "${body}" || {
  echo 'the body did not survive the read unchanged' >&2
  exit 1
}

# The same body behind two 503s. Retrying is the fix's first half; without it
# this is the run that refuses a correct body.
arrange "${body}" 2
run_reader
test "${status}" = 0 || {
  printf 'a transient 503 was not retried through to a successful read: %s\n' "${stderr}" >&2
  exit 1
}
test "${stdout}" = "${body}"
test "$(cat "${FAKE_GH_ATTEMPTS}")" = 3

# An outage that outlasts the retries fails as a *read*, and says so. This is
# the assertion the whole script exists for: before the fix this case exited 0
# from `gh` into an empty pipe and was reported against the author's body.
arrange "${body}" 99
run_reader
test "${status}" != 0
grep -q 'could not read pull request 7' <<<"${stderr}" || {
  printf 'a failed read was not reported as a failed read: %s\n' "${stderr}" >&2
  exit 1
}
grep -q 'not a verdict on the body' <<<"${stderr}"
test -z "${stdout}"

# A permanent failure is not retried into a slow version of the same answer.
arrange "${body}" 99 "${permanent}"
run_reader
test "${status}" != 0
test "$(cat "${FAKE_GH_ATTEMPTS}")" = 1 || {
  printf 'a 403 was retried %s times instead of reported at once\n' "$(cat "${FAKE_GH_ATTEMPTS}")" >&2
  exit 1
}

# A body with nothing in it is refused, and refused for being empty.
for empty in '' '   ' $'\n\n'; do
  arrange "${empty}" 0
  run_reader
  test "${status}" != 0 || {
    printf 'an empty body (%q) was not refused\n' "${empty}" >&2
    exit 1
  }
  grep -q 'has an empty body' <<<"${stderr}" || {
    printf 'an empty body (%q) was refused with the wrong message: %s\n' "${empty}" "${stderr}" >&2
    exit 1
  }
done

# A pull request opened with no description at all. The API answers JSON null,
# not an empty string, and `jq -r .body` renders that as the four characters
# "null" -- which is not empty, reaches the verifier as prose, and is refused
# for missing a `## Outcome` section. The same misdiagnosis, one layer down.
# The `// ""` in the reader's filter is what puts it on the branch above.
arrange_response '{"body": null}' 0
run_reader
test "${status}" != 0 || {
  printf 'a pull request with no description was accepted, and the verifier would have been handed %q\n' "${stdout}" >&2
  exit 1
}
grep -q 'has an empty body' <<<"${stderr}" || {
  printf 'a null body was not recognised as empty: %s\n' "${stderr}" >&2
  exit 1
}

# ...and an empty body is told apart from a failed read, which is the
# distinction that sends one person to re-run the gate and another to write a
# description. Asserted as an absence because the two messages are what a human
# acts on.
arrange '' 0
run_reader
grep -q 'could not read' <<<"${stderr}" && {
  echo 'an empty body was reported as a failed read' >&2
  exit 1
}

# A pull request number that never got set. Left unchecked this sends
# `repos/owner/repo/pulls/` to the API and verifies whatever it answers.
for bad in '' '0' 'abc' '12/../13'; do
  set +e
  GITHUB_REPOSITORY=owner/repo PULL_REQUEST_NUMBER="${bad}" \
    "${reader}" >/dev/null 2>"${test_dir}/stderr"
  status=$?
  set -e
  test "${status}" != 0 || {
    printf 'a pull request number of %q was accepted\n' "${bad}" >&2
    exit 1
  }
  grep -q 'PULL_REQUEST_NUMBER must be' <"${test_dir}/stderr"
done

# Running outside GitHub Actions is an error rather than a read against an
# empty repository path.
set +e
GITHUB_REPOSITORY= PULL_REQUEST_NUMBER=7 "${reader}" >/dev/null 2>"${test_dir}/stderr"
status=$?
set -e
test "${status}" != 0
grep -q 'GITHUB_REPOSITORY is empty' <"${test_dir}/stderr"

# The end-to-end shape the workflow runs: a read that fails must not reach the
# verifier at all. Before the fix this pipeline exited 0 on a 503 answering an
# incomplete body, and 1 with the wrong message on a 503 answering a good one.
arrange "${body}" 99
set +e
verifier_output="$(
  GITHUB_REPOSITORY=owner/repo PULL_REQUEST_NUMBER=7 \
    bash -c 'set -euo pipefail
      body="$("$1")"
      "$2" <<<"${body}"' _ "${reader}" "${repo_root}/scripts/verify-pr-fidelity.sh" 2>&1
)"
status=$?
set -e
test "${status}" != 0
grep -q 'is missing or empty' <<<"${verifier_output}" && {
  printf 'a failed read still reached the verifier and was blamed on the body: %s\n' "${verifier_output}" >&2
  exit 1
}

# Release Please overflow bodies point to `release-notes.md` on the companion
# `<headRef>--release-notes` branch when the changelog exceeds GitHub's 65536
# byte PR body limit (#844).
overflow_body='This release is too large to preview in the pull request body. View the full release notes here: https://github.com/owner/repo/blob/release-please--branches--main--components--conduit--release-notes/release-notes.md'
encoded_notes="$(printf '%s' "${body}" | base64 | tr -d '\r\n')"
printf '%s' "$(jq -n --arg body "${overflow_body}" --arg content "${encoded_notes}" '{body: $body, content: $content}')" > "${FAKE_GH_RESPONSE}"
printf '0\n' > "${FAKE_GH_FAILURES}"
rm -f "${FAKE_GH_ATTEMPTS}"
run_reader
test "${status}" = 0 || {
  printf 'a Release Please overflow pointer was not resolved to its release-notes.md content: %s\n' "${stderr}" >&2
  exit 1
}
test "${stdout}" = "${body}" || {
  echo 'the resolved overflow body did not match the stored release-notes.md' >&2
  exit 1
}
test "$(cat "${FAKE_GH_ATTEMPTS}")" = 2

echo 'read-pull-request-body: all cases passed'

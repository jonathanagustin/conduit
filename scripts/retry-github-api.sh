#!/usr/bin/env bash
# Retries a GitHub API call through a transient server-side failure.
#
# Written after run 49 of the Trunk gate, where `POST /statuses/{sha}` returned
# 503 -- "No server is currently available to service your request" -- and the
# step, running under `bash -e`, exited 1 before a single gate had run. Thirty
# verifications were skipped because a piece of telemetry about the run could
# not be written. The same call at the end of the job would have turned a green
# run red for the same reason.
#
# Retrying is half the fix and the smaller half; the workflow marks those steps
# `continue-on-error` so a status that cannot be published never decides whether
# the code is good. This handles the common case, which is a blip of a few
# seconds rather than the twenty minutes run 49 hit.
#
# It deliberately does NOT retry everything. `retry-keyless-signing.sh` does,
# because a keyless signing failure is nearly always Fulcio or Rekor being
# briefly unavailable. Here the plausible failures are split: 5xx and 429 are
# GitHub, and waiting helps; 401, 403 and 422 are the token, the permissions
# block or the request, and waiting only converts a clear error into the same
# error thirty seconds later. Under `continue-on-error` that difference matters
# more than usual, because a permanent failure would otherwise be swallowed by
# the retry and then swallowed again by the step -- so it is reported at once,
# loudly, with the response body that explains it.
set -uo pipefail

if (( $# == 0 )); then
  echo "usage: retry-github-api.sh COMMAND [ARG ...]" >&2
  exit 64
fi

max_attempts="${GITHUB_API_MAX_ATTEMPTS:-4}"
delay_seconds="${GITHUB_API_INITIAL_DELAY_SECONDS:-2}"

if [[ ! "${max_attempts}" =~ ^[1-9][0-9]*$ ]] || (( max_attempts > 10 )); then
  echo "GITHUB_API_MAX_ATTEMPTS must be an integer from 1 through 10" >&2
  exit 64
fi
if [[ ! "${delay_seconds}" =~ ^[0-9]+$ ]] || (( delay_seconds > 300 )); then
  echo "GITHUB_API_INITIAL_DELAY_SECONDS must be an integer from 0 through 300" >&2
  exit 64
fi

output="$(mktemp)"
trap 'rm -f "${output}"' EXIT

# What counts as worth waiting for. gh reports the status in its message rather
# than its exit code -- every API failure is exit 1 -- so this reads the text.
# Case-insensitively: gh capitalises its prose ("No server is currently
# available...") while the HTTP status is upper case, and matching one spelling
# would quietly stop retrying the other. The non-HTTP patterns are for a call
# that never reached GitHub at all -- a dropped connection or a DNS blip prints
# no status and is as transient as a 503.
is_transient() {
  grep -qiE 'HTTP (5[0-9][0-9]|429)|no server is currently available|connection reset|i/o timeout|TLS handshake timeout|temporary failure in name resolution' \
    <"${output}"
}

for ((attempt = 1; attempt <= max_attempts; attempt++)); do
  # The status is captured in an else branch rather than after the `if`, which
  # reads as equivalent and is not: an `if` whose condition fails and which has
  # no else is itself a command that succeeded, so `$?` afterwards is 0. Written
  # that way this script exited 0 after exhausting every retry -- reporting a
  # published status that was never published. The self-test plants that case.
  if "$@" >"${output}" 2>&1; then
    cat "${output}"
    exit 0
  else
    status=$?
  fi

  if ! is_transient; then
    echo "GitHub API call failed and the failure is not transient; not retrying:" >&2
    cat "${output}" >&2
    exit "${status}"
  fi

  if (( attempt == max_attempts )); then
    echo "GitHub API call failed after ${attempt} attempts:" >&2
    cat "${output}" >&2
    exit "${status}"
  fi

  echo "GitHub API call failed transiently (attempt ${attempt}/${max_attempts}); retrying in ${delay_seconds}s" >&2
  cat "${output}" >&2
  sleep "${delay_seconds}"
  delay_seconds=$((delay_seconds * 2))
done

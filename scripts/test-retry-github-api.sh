#!/usr/bin/env bash
# Proves retry-github-api.sh tells a transient GitHub failure from a permanent
# one. Both halves matter: retrying a 403 wastes half a minute and then reports
# the same error, and *not* retrying a 503 is what skipped thirty gates in run
# 49 of the Trunk gate.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
retry="${repo_root}/scripts/retry-github-api.sh"
test_dir="$(mktemp -d)"
trap 'rm -rf "${test_dir}"' EXIT

# A stand-in gh: fails with the given message until it has been called `succeed
# _after` times, then succeeds. Counting through a file rather than a variable
# because each attempt is a fresh process.
cat > "${test_dir}/fake-gh.sh" <<'SCRIPT'
#!/usr/bin/env bash
set -uo pipefail
counter="$1"; succeed_after="$2"; message="$3"
count=0
if [[ -f "${counter}" ]]; then count="$(cat "${counter}")"; fi
count=$((count + 1))
printf '%s\n' "${count}" > "${counter}"
if (( count > succeed_after )); then
  echo 'status published'
  exit 0
fi
printf '%s\n' "${message}" >&2
exit 1
SCRIPT
chmod 0755 "${test_dir}/fake-gh.sh"

run_retry() {
  GITHUB_API_MAX_ATTEMPTS="$1" GITHUB_API_INITIAL_DELAY_SECONDS=0 \
    "${retry}" "${test_dir}/fake-gh.sh" "$2" "$3" "$4"
}

# The exact message run 49 died on. It is quoted verbatim rather than
# paraphrased: this gate exists because that string was not recognised, and a
# reworded copy would pass while the real one still failed.
transient='gh: No server is currently available to service your request. Sorry about that. Please try resubmitting your request and contact us if the problem persists. (HTTP 503)'

output="$(run_retry 4 "${test_dir}/transient" 2 "${transient}")"
grep -q 'status published' <<<"${output}" || {
  echo 'a transient 503 was not retried through to success' >&2
  exit 1
}
test "$(cat "${test_dir}/transient")" = 3

# Retries are bounded. A GitHub outage that outlasts the backoff must end as a
# reported failure rather than a job that sits there doubling its sleep.
set +e
run_retry 3 "${test_dir}/forever" 99 "${transient}" >/dev/null 2>&1
status=$?
set -e
test "${status}" = 1
test "$(cat "${test_dir}/forever")" = 3

# A permanent failure is reported on the first attempt. Retrying a revoked
# token or a rejected request only delays the same answer -- and because the
# workflow marks these steps continue-on-error, a slow permanent failure is a
# real error hidden twice over.
for permanent in \
  'gh: Resource not accessible by integration (HTTP 403)' \
  'gh: Bad credentials (HTTP 401)' \
  'gh: Validation Failed (HTTP 422)'; do
  counter="${test_dir}/permanent-${RANDOM}"
  set +e
  run_retry 4 "${counter}" 99 "${permanent}" >/dev/null 2>&1
  status=$?
  set -e
  test "${status}" = 1
  test "$(cat "${counter}")" = 1 || {
    printf 'a permanent failure was retried: %s\n' "${permanent}" >&2
    exit 1
  }
done

# The same outage without a status code to match on. Run 49's log carries two
# lines: gh's message, which ends in "(HTTP 503)", and the bare response body,
# which does not. A wrapper matching only the code would stop retrying the
# moment gh printed the body alone -- and matching the prose case-sensitively
# would miss it too, because gh capitalises the sentence. Both halves of that
# are what this case holds down; without it the -i survives being removed.
run_retry 3 "${test_dir}/prose" 1 \
  '{"message": "No server is currently available to service your request. Sorry about that."}' >/dev/null
test "$(cat "${test_dir}/prose")" = 2

# Rate limiting is transient even though it is a 4xx, which is the one case the
# 5xx rule alone would get wrong.
run_retry 3 "${test_dir}/throttled" 1 'gh: API rate limit exceeded (HTTP 429)' >/dev/null
test "$(cat "${test_dir}/throttled")" = 2

# Configuration is validated rather than silently defaulted, so a typo in the
# workflow cannot turn retries off without anyone noticing.
if GITHUB_API_MAX_ATTEMPTS=0 "${retry}" true; then
  echo 'invalid retry configuration unexpectedly succeeded' >&2
  exit 1
fi
if "${retry}"; then
  echo 'a missing command unexpectedly succeeded' >&2
  exit 1
fi

# The wrapped command's own output still reaches the caller on success; a
# wrapper that swallowed it would make every future use of this script lossy.
grep -q 'status published' <<<"$(run_retry 2 "${test_dir}/quiet" 0 "${transient}")" || {
  echo 'the wrapped command output was swallowed' >&2
  exit 1
}

echo 'GitHub API retry behavior verified'

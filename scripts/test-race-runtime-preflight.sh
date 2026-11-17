#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/bin"

# This compiler double supplies process outcomes, not a race implementation.
# Real native and emulator controls exercise the generated Go fixture separately.
cat > "$work/bin/go" <<'GO_DOUBLE'
#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == env ]]; then
  case "${2:-}" in GOVERSION) echo go1.27.4 ;; GOOS) echo linux ;; GOARCH) echo amd64 ;; esac
  exit 0
fi
[[ "${1:-}" == test && " $* " == *' -race '* && " $* " == *' -c '* ]] || exit 97
[[ "${SCENARIO}" != compile-failure ]] || { echo 'injected compiler failure'; exit 1; }
output=
while (($#)); do
  if [[ "$1" == -o ]]; then output="$2"; break; fi
  shift
done
[[ -n "$output" ]] || exit 98
cat > "$output" <<'TEST_DOUBLE'
#!/usr/bin/env bash
set -euo pipefail
name="$2"
name="${name#^}"
name="${name%\$}"
printf '%s\n' "$name" >> "$TRACE"
if [[ "$SCENARIO" == startup-zero || "$SCENARIO" == startup-with-markers ]]; then
  echo 'SanitizerTool: CHECK failed: sanitizer_allocator_primary32.h:292'
  if [[ "$SCENARIO" == startup-with-markers ]]; then
    echo "=== RUN   $name"
    echo CONDUIT_RACE_PREFLIGHT_POSITIVE_BODY
    echo "--- PASS: $name (0.00s)"
    echo PASS
  fi
  exit 0
fi
[[ "$SCENARIO" != missing-run ]] || exit 0
echo "=== RUN   $name"
case "$name" in
  TestPositive)
    [[ "$SCENARIO" == missing-positive-body ]] || echo CONDUIT_RACE_PREFLIGHT_POSITIVE_BODY
    echo '--- PASS: TestPositive (0.00s)'
    echo PASS
    [[ "$SCENARIO" != positive-nonzero ]] || exit 1
    exit 0
    ;;
  TestUnconditionalFailure)
    echo CONDUIT_RACE_PREFLIGHT_FAILURE_BODY
    echo '--- FAIL: TestUnconditionalFailure (0.00s)'
    echo FAIL
    [[ "$SCENARIO" != unconditional-zero ]] || exit 0
    exit 1
    ;;
  TestIntentionalRace)
    [[ "$SCENARIO" == missing-race-body ]] || echo CONDUIT_RACE_PREFLIGHT_RACE_BODY
    [[ "$SCENARIO" == missing-race-warning ]] || echo 'WARNING: DATA RACE'
    if [[ "$SCENARIO" != halt-on-error ]]; then
      echo '--- FAIL: TestIntentionalRace (0.00s)'
      echo FAIL
    fi
    [[ "$SCENARIO" != race-zero ]] || exit 0
    exit 66
    ;;
  *) exit 99 ;;
esac
TEST_DOUBLE
chmod 755 "$output"
GO_DOUBLE
chmod 755 "$work/bin/go"

cases=0
check() {
  local scenario="$1" expected="$2" diagnostic="${3:-}" trace="$work/$1.trace" result
  : > "$trace"
  if SCENARIO="$scenario" TRACE="$trace" PATH="$work/bin:$PATH" \
      bash "$root/scripts/verify-race-runtime.sh" > "$work/$scenario.log" 2>&1; then
    result=0
  else
    result=$?
  fi
  if [[ "$expected" == pass ]]; then
    [[ "$result" -eq 0 ]] || { cat "$work/$scenario.log" >&2; exit 1; }
    printf '%s\n' TestPositive TestUnconditionalFailure TestIntentionalRace > "$work/expected.trace"
    cmp -s "$trace" "$work/expected.trace" || { echo 'a required runtime control was omitted or reordered' >&2; exit 1; }
  else
    [[ "$result" -ne 0 ]] || { echo "accepted broken runtime: $scenario" >&2; exit 1; }
    grep -qF -- "$diagnostic" "$work/$scenario.log" || { cat "$work/$scenario.log" >&2; echo "wrong refusal for $scenario" >&2; exit 1; }
  fi
  cases=$((cases + 1))
}

check valid pass
check halt-on-error pass
check compile-failure fail 'could not compile the race controls'
check startup-zero fail 'sanitizer startup failed (exit 0)'
check startup-with-markers fail 'sanitizer startup failed (exit 0)'
check missing-run fail 'required test body did not execute'
check missing-positive-body fail 'required test body did not execute'
check positive-nonzero fail 'positive control did not pass'
check unconditional-zero fail 'unconditional failure was not rejected'
check missing-race-body fail 'required test body did not execute'
check missing-race-warning fail 'intentional race was not detected and rejected'
check race-zero fail 'intentional race was not detected and rejected'
printf 'race runtime preflight rejects false success and preserves all controls (%d cases)\n' "$cases"

# The preflight must remain before the suite in its Go job and in the complete
# local verifier. Both script invocations also participate in reverse gate drift.
awk '
  /^  go:$/ { lane = 1; next }
  lane && /^  [A-Za-z0-9_-]+:$/ { exit }
  lane && /^      - name: Validate race runtime preflight$/ { preflight = NR }
  lane && /^      - name: Test$/ { suite = NR }
  END { if (!preflight || !suite || preflight >= suite) exit 1 }
' "$root/.github/workflows/platform-ci.yml" || { echo 'workflow does not prove the race runtime before its Go suite' >&2; exit 1; }
preflight_line="$(grep -nF "gate 'Validate race runtime preflight'" "$root/scripts/verify-local.sh" | cut -d: -f1)"
suite_line="$(grep -nF "gate 'Test'" "$root/scripts/verify-local.sh" | cut -d: -f1)"
[[ -n "$preflight_line" && -n "$suite_line" && "$preflight_line" -lt "$suite_line" ]] || {
  echo 'complete local verifier does not prove the race runtime before its Go suite' >&2
  exit 1
}
workflow_code="$(grep -vE '^[[:space:]]*#' "$root/.github/workflows/platform-ci.yml")"
for script in test-race-runtime-preflight.sh verify-race-runtime.sh; do
  grep -qF "$script" <<<"$workflow_code" || {
    echo "workflow omits $script from gate reconciliation" >&2
    exit 1
  }
done

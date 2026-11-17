#!/usr/bin/env bash
# Prove the race runtime executes tests before a zero exit can count as a pass.
set -euo pipefail
[[ $# -eq 0 ]] || { echo 'usage: verify-race-runtime.sh' >&2; exit 2; }
command -v go >/dev/null || { echo 'Go is required for the race runtime preflight' >&2; exit 1; }
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

cat > "$work/race_runtime_test.go" <<'GO'
package racepreflight

import (
	"sync"
	"testing"
)

func TestPositive(t *testing.T) {
	var lock sync.Mutex
	var workers sync.WaitGroup
	total := 0
	workers.Add(4)
	for range 4 {
		go func() {
			defer workers.Done()
			for range 1000 {
				lock.Lock()
				total++
				lock.Unlock()
			}
		}()
	}
	workers.Wait()
	if total != 4000 {
		t.Fatalf("protected total = %d", total)
	}
	t.Log("CONDUIT_RACE_PREFLIGHT_POSITIVE_BODY")
}

func TestUnconditionalFailure(t *testing.T) {
	t.Fatal("CONDUIT_RACE_PREFLIGHT_FAILURE_BODY")
}

var unprotected int

func TestIntentionalRace(t *testing.T) {
	t.Log("CONDUIT_RACE_PREFLIGHT_RACE_BODY")
	var workers sync.WaitGroup
	start := make(chan struct{})
	workers.Add(2)
	for range 2 {
		go func() {
			defer workers.Done()
			<-start
			for range 10000 {
				unprotected++
			}
		}()
	}
	close(start)
	workers.Wait()
}
GO

fail() {
  echo "race runtime preflight: $1" >&2
  [[ $# -lt 2 ]] || cat "$2" >&2
  exit 1
}

if ! go test -race -c -o "$work/race-runtime.test" "$work/race_runtime_test.go" > "$work/compile.log" 2>&1; then
  fail 'could not compile the race controls with the current Go target' "$work/compile.log"
fi

check_control() {
  local name="$1" marker="$2" expected="$3" log="$work/$1.log" result
  if "$work/race-runtime.test" -test.run "^${name}$" -test.v -test.timeout=30s > "$log" 2>&1; then
    result=0
  else
    result=$?
  fi
  if grep -Eq 'SanitizerTool:|FATAL: ThreadSanitizer|ThreadSanitizer.*(CHECK failed|failed to|unexpected memory)' "$log"; then
    fail "$name: sanitizer startup failed (exit $result); no race verification was established" "$log"
  fi
  grep -qxF -- "=== RUN   $name" "$log" &&
    grep -qE -- "^[[:space:]]+race_runtime_test[.]go:[0-9]+: ${marker}$" "$log" ||
    fail "$name: the required test body did not execute (exit $result)" "$log"
  case "$expected" in
    pass)
      [[ "$result" -eq 0 ]] && grep -qE -- "^--- PASS: ${name} \\([0-9]+([.][0-9]+)?s\\)$" "$log" && grep -qx 'PASS' "$log" ||
        fail "$name: the positive control did not pass" "$log"
      ;;
    fail)
      [[ "$result" -ne 0 ]] && grep -qE -- "^--- FAIL: ${name} \\([0-9]+([.][0-9]+)?s\\)$" "$log" ||
        fail "$name: the unconditional failure was not rejected" "$log"
      ;;
    race)
      # halt_on_error may exit before testing prints FAIL. The body marker,
      # actual detector warning and nonzero exit still prove race detection.
      [[ "$result" -ne 0 ]] && grep -qxF -- 'WARNING: DATA RACE' "$log" ||
        fail "$name: the intentional race was not detected and rejected" "$log"
      ;;
  esac
}

check_control TestPositive CONDUIT_RACE_PREFLIGHT_POSITIVE_BODY pass
check_control TestUnconditionalFailure CONDUIT_RACE_PREFLIGHT_FAILURE_BODY fail
check_control TestIntentionalRace CONDUIT_RACE_PREFLIGHT_RACE_BODY race
printf 'race runtime executed the positive, unconditional-failure and intentional-race controls (%s, target %s/%s)\n' \
  "$(go env GOVERSION)" "$(go env GOOS)" "$(go env GOARCH)"

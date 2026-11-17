#!/usr/bin/env bash
# Consume the Go test framework events, never human log substrings. An exit-0
# go test with no matching tests, t.Skip, or a package-only PASS is not evidence.
set -euo pipefail
package='github.com/jonathanagustin/conduit/internal/gitless'
jq -se --arg package "${package}" '
  ["TestAcceptanceARealSignedChannelReachesARealCluster",
   "TestAcceptanceARealSignatureIsRefusedUnderAForeignPolicy",
   "TestAcceptanceReconcilingTwiceDeploysOnce"] as $expected |
  length > 0 and all(.[]; .Package == $package and (.Action | type) == "string") and
  all(.[]; .Action != "skip" and .Action != "fail") and
  ([.[] | select(.Action == "run") | .Test] | sort) == ($expected | sort) and
  ([.[] | select(.Action == "pass" and has("Test")) | .Test] | sort) == ($expected | sort) and
  ([.[] | select(.Action == "pass" and (has("Test") | not))] | length) == 1
' "$1" >/dev/null || {
  jq -r 'select(.Action == "skip" or .Action == "fail") | "\(.Action): \(.Test // .Package)"' "$1" >&2 || true
  echo 'gitless acceptance: every expected test must run and pass; skipped, missing, failed or malformed events refuse qualification' >&2
  exit 1
}
jq -r 'select(.Action == "output") | .Output // empty' "$1"

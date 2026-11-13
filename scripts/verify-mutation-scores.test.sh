#!/usr/bin/env bash
# Proves the mutation-scores ratchet gate rejects a rotten baseline.
#
# verify-mutation-scores.mjs is the offline half of the mutation gate: it runs
# on every candidate and cannot re-measure efficacy, so its whole job is to
# refuse a baseline that has stopped meaning what it says -- hand-edited upward,
# left behind while the code moved, or gone stale. A gate that only ever passes
# is indistinguishable from no gate. This exercises each refusal on a crafted
# baseline pointed at through CONDUIT_MUTATION_SCORES, and confirms the real
# committed baseline still passes.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"

gate="scripts/verify-mutation-scores.mjs"
tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT

# A real package and a real web source file on disk, so a rejection is about
# the field under test rather than a coincidentally missing path.
real_pkg="internal/governance/domain"
real_web="web/src/api/protocol.ts"

# Run the gate against a baseline file, with a fixed "today" so staleness is
# deterministic. Prints nothing; returns the gate's exit code.
run_gate() {
  local scores="$1" today="${2:-2026-08-16}"
  CONDUIT_MUTATION_SCORES="${scores}" CONDUIT_MUTATION_TODAY="${today}" \
    node "${gate}" >/dev/null 2>&1
}

# Assert the gate REFUSES the given baseline. Fails the test if it passes.
must_reject() {
  local label="$1" scores="$2" today="${3:-2026-08-16}"
  if run_gate "${scores}" "${today}"; then
    echo "FAIL: the gate accepted ${label}" >&2
    exit 1
  fi
  echo "  refused: ${label}"
}

# Assert the gate ACCEPTS the given baseline.
must_accept() {
  local label="$1" scores="$2" today="${3:-2026-08-16}"
  if ! run_gate "${scores}" "${today}"; then
    echo "FAIL: the gate rejected ${label}, which is well-formed" >&2
    CONDUIT_MUTATION_SCORES="${scores}" CONDUIT_MUTATION_TODAY="${today}" node "${gate}" >&2 || true
    exit 1
  fi
  echo "  accepted: ${label}"
}

# A well-formed baseline captured "today", used as the base each case mutates.
# It carries both a Go package and a web file so the two existence checks and
# the shared consistency checks are all exercised.
well_formed() {
  cat <<JSON
{
  "note": "self-test fixture",
  "capturedAt": "2026-08-16",
  "packages": {
    "${real_pkg}": { "efficacy": 80, "killed": 8, "lived": 2, "total": 10, "coverage": 90 }
  },
  "web": {
    "${real_web}": { "efficacy": 85, "killed": 17, "lived": 3, "total": 20, "coverage": 100 }
  }
}
JSON
}

echo "verify-mutation-scores: the ratchet must refuse a rotten baseline"

# --- the sanity anchor: a well-formed baseline passes -----------------------
good="${tmp}/good.json"
well_formed > "${good}"
must_accept "a well-formed baseline" "${good}"

# --- missing file -----------------------------------------------------------
must_reject "a baseline file that does not exist" "${tmp}/absent.json"

# --- not JSON ---------------------------------------------------------------
printf 'this is not json {' > "${tmp}/garbage.json"
must_reject "a file that is not valid JSON" "${tmp}/garbage.json"

# --- no packages ------------------------------------------------------------
printf '{"capturedAt":"2026-08-16","packages":{}}' > "${tmp}/empty.json"
must_reject "a baseline recording no packages" "${tmp}/empty.json"

# --- a package that is not on disk ------------------------------------------
well_formed | sed "s#${real_pkg}#internal/does/not/exist#" > "${tmp}/ghost.json"
must_reject "a package that is not a Go package on disk" "${tmp}/ghost.json"

# --- a web file that is not on disk -----------------------------------------
well_formed | sed "s#${real_web}#web/src/does/not/exist.ts#" > "${tmp}/webghost.json"
must_reject "a web source file that is not on disk" "${tmp}/webghost.json"

# --- a web efficacy inconsistent with its counts ----------------------------
# 17 killed of 20 is 85, not 42; the derivation check applies to web too.
well_formed | sed 's/"efficacy": 85/"efficacy": 42/' > "${tmp}/webinflated.json"
must_reject "a web efficacy inconsistent with its killed and lived counts" "${tmp}/webinflated.json"

# --- efficacy that does not follow from killed and lived --------------------
# 8 killed of 10 is 80, not 99; a hand-edited efficacy is exactly the tampering
# the derivation check exists to catch.
well_formed | sed 's/"efficacy": 80/"efficacy": 99/' > "${tmp}/inflated.json"
must_reject "an efficacy inconsistent with its killed and lived counts" "${tmp}/inflated.json"

# --- total less than killed+lived -------------------------------------------
well_formed | sed 's/"total": 10/"total": 3/' > "${tmp}/undercount.json"
must_reject "a total smaller than killed plus lived" "${tmp}/undercount.json"

# --- a non-numeric field ----------------------------------------------------
well_formed | sed 's/"coverage": 90/"coverage": "high"/' > "${tmp}/nonnumeric.json"
must_reject "a non-numeric score field" "${tmp}/nonnumeric.json"

# --- capturedAt not a real date ---------------------------------------------
well_formed | sed 's/"capturedAt": "2026-08-16"/"capturedAt": "someday"/' > "${tmp}/undated.json"
must_reject "a capturedAt that is not a YYYY-MM-DD date" "${tmp}/undated.json"

# --- stale: captured more than 90 days before today -------------------------
# The fixture is dated 2026-08-16; a "today" far enough ahead makes it stale.
must_reject "a baseline older than the 90-day maximum" "${good}" "2027-01-01"

# --- future: captured after today -------------------------------------------
must_reject "a baseline captured in the future" "${good}" "2026-01-01"

# --- the real committed baseline still passes -------------------------------
# The gate is only worth wiring in if it accepts the tree it ships with.
must_accept "the committed docs/operations/mutation-scores.json" \
  "docs/operations/mutation-scores.json"

echo "verify-mutation-scores: all cases behaved"

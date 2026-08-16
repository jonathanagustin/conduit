#!/usr/bin/env bash
# Proves the review-ledger gate rejects each way a row goes bad, FOR THE RIGHT
# REASON.
#
# An earlier revision of this file claimed to plant "one violation of each rule"
# and did neither half honestly. It planted seven cases against roughly thirteen
# rules, and its marquee case -- the short sha -- used
# `gsub(/([0-9a-f]{7})[0-9a-f]{33}/, "\\1", $1)`, which no awk supports: mawk
# writes the two literal characters \1, so the planted URL was `blob/\1/...`, a
# garbage ref rather than a short sha. The gate rejected it through the intended
# branch for unintended input, and the proof that this mattered was blunt:
# loosening the gate to accept genuine short shas still passed this self-test.
# The one rule the gate tells a story about was the one rule its own test did
# not pin.
#
# Two changes answer that. Planting uses sed, which does support backreferences,
# so the short-sha case plants an actual short sha and asserts it did. And every
# case asserts the gate's MESSAGE, not merely its exit status, so a rejection
# arriving from a different rule fails here instead of passing quietly.
#
# It still never edits a tracked file: the gate takes the ledger path as an
# argument, so each case is a throwaway copy and `git checkout --` is never
# needed. The copy lives beside the real ledger only because `topic` resolves
# relative to the ledger's own directory.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"

gate="scripts/verify-review-ledger.mjs"
ledger="docs/apollo/reference/review-ledger.tsv"
work="$(mktemp "docs/apollo/reference/.review-ledger-selftest.XXXXXX")"
cleanup() { rm -f "${work}"; }
trap cleanup EXIT

echo "verify-review-ledger: the gate must reject bad rows for the stated reason"

if ! node "${gate}" "${ledger}" >/dev/null 2>&1; then
  echo "FAIL: the gate does not pass on the committed ledger" >&2
  node "${gate}" "${ledger}" >&2 || true
  exit 1
fi
echo "  passes: the committed ledger"

cp "${ledger}" "${work}"
if ! node "${gate}" "${work}" >/dev/null 2>&1; then
  echo "FAIL: an unmodified copy of the ledger does not pass" >&2
  exit 1
fi
echo "  passes: an unmodified copy"

# reject <label> <expected message fragment>
# The fragment is what makes this a test rather than a smoke check: an exit
# status alone cannot tell one rule firing from another.
reject() {
  local label="$1" expected="$2" output
  if output="$(node "${gate}" "${work}" 2>&1)"; then
    echo "FAIL: the gate accepted ${label}" >&2
    exit 1
  fi
  if ! printf '%s' "${output}" | grep -qF -- "${expected}"; then
    echo "FAIL: ${label} was rejected, but not for the stated reason" >&2
    echo "      expected the message to contain: ${expected}" >&2
    printf '%s\n' "${output}" | sed 's/^/      /' >&2
    exit 1
  fi
  echo "  rejects: ${label}"
}

# --- a digest that is not 64 hex --------------------------------------------
# Hex but short, so a gate loosened to accept 8-64 hex fails here too.
awk -F'\t' 'BEGIN{OFS="\t"} NR==2{$3="abcdef0123456789"} {print}' "${ledger}" > "${work}"
reject "a snapshot_sha256 that is not 64 hex characters" "is not 64 lowercase hex characters"

# --- a topic that resolves to nothing ---------------------------------------
awk -F'\t' 'BEGIN{OFS="\t"} NR==2{$4="no-such-note.md"} {print}' "${ledger}" > "${work}"
reject "a topic that resolves to no file (orphaned evidence)" "the evidence is orphaned"

# --- a GitHub reference pinned to a short sha -------------------------------
# sed, not awk: awk has no backreference in the replacement, which is exactly
# how the previous revision planted a garbage ref and proved nothing.
sed -E 's#/(blob|tree)/([0-9a-f]{7})[0-9a-f]{33}#/\1/\2#' "${ledger}" > "${work}"
grep -qE '/(blob|tree)/[0-9a-f]{7}/' "${work}" || {
  echo "FAIL: the short-sha planting did not produce a short sha" >&2
  exit 1
}
reject "a GitHub reference pinned to a short sha" "rather than a full 40-character commit sha"

# --- a raw.githubusercontent URL pinned to a branch -------------------------
awk -F'\t' 'BEGIN{OFS="\t"} NR==2{$1="https://raw.githubusercontent.com/palantir/osdk-ts/main/README.md"} {print}' "${ledger}" > "${work}"
reject "a raw.githubusercontent URL pinned to a branch" "rather than a full 40-character commit sha"

# --- a duplicated (url, topic) pair -----------------------------------------
{ cat "${ledger}"; awk 'NR==2' "${ledger}"; } > "${work}"
reject "a duplicated (url, topic) pair" "duplicates the (url, topic) pair"

# --- a row with too few fields ----------------------------------------------
awk -F'\t' 'BEGIN{OFS="\t"} NR==2{print $1,$2,$3,$4; next} {print}' "${ledger}" > "${work}"
reject "a row with four fields instead of five" "has 4 fields, expected 5"

# --- a row with too many fields ---------------------------------------------
awk -F'\t' 'BEGIN{OFS="\t"} NR==2{print $1,$2,$3,$4,$5,"extra"; next} {print}' "${ledger}" > "${work}"
reject "a row with six fields instead of five" "has 6 fields, expected 5"

# --- a renamed header column ------------------------------------------------
awk -F'\t' 'BEGIN{OFS="\t"} NR==1{$3="sha"} {print}' "${ledger}" > "${work}"
reject "a renamed header column" "expected [url, reviewed_at_utc"

# --- an empty disposition ---------------------------------------------------
awk -F'\t' 'BEGIN{OFS="\t"} NR==2{$5=""} {print}' "${ledger}" > "${work}"
reject "a row that records no disposition" "disposition is empty"

# --- an empty topic ---------------------------------------------------------
awk -F'\t' 'BEGIN{OFS="\t"} NR==2{$4=""} {print}' "${ledger}" > "${work}"
reject "a row that names no note" "names no note it supports"

# --- a date shaped right that is not a real day -----------------------------
awk -F'\t' 'BEGIN{OFS="\t"} NR==2{$2="2026-99-99"} {print}' "${ledger}" > "${work}"
reject "a review date that is not a real calendar day" "is not a real YYYY-MM-DD date"

# --- a non-https URL --------------------------------------------------------
awk -F'\t' 'BEGIN{OFS="\t"} NR==2{$1="http://example.com/x"} {print}' "${ledger}" > "${work}"
reject "a url that is not https" "is not an absolute https URL"

# --- a blank row ------------------------------------------------------------
awk 'NR==2{print ""} {print}' "${ledger}" > "${work}"
reject "a blank row" "blank row"

# --- a header with no rows at all -------------------------------------------
# The gate reported "0 rows" and exit 0 before this case existed, so deleting
# every row of guarded evidence was invisible.
head -1 "${ledger}" > "${work}"
reject "a ledger emptied down to its header" "the evidence it indexes is gone"

# --- restored, the real ledger still passes ---------------------------------
if ! node "${gate}" "${ledger}" >/dev/null 2>&1; then
  echo "FAIL: the committed ledger stopped passing" >&2
  exit 1
fi
echo "  passes: the committed ledger, unchanged throughout"

echo "verify-review-ledger: all cases behaved"

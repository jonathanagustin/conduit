#!/usr/bin/env bash
# Proves the review-ledger gate rejects each way a row stops being re-checkable.
#
# A gate that only ever passes is indistinguishable from no gate, so this plants
# one violation of each rule and requires a rejection for every one.
#
# Unlike the console-contract self-test, this one never edits a tracked file.
# The gate takes the ledger path as an argument, so each case is a throwaway
# copy and `git checkout --` is never needed. AGENTS.md records what that
# command costs when a harness reaches for it -- it cannot tell your work from
# the planted mutation, and it discards both. Not needing it is better than
# guarding it, so the copy lives beside the real ledger only because `topic` is
# resolved relative to the ledger's own directory.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"

gate="scripts/verify-review-ledger.mjs"
ledger="docs/apollo/reference/review-ledger.tsv"
work="$(mktemp "docs/apollo/reference/.review-ledger-selftest.XXXXXX")"
cleanup() { rm -f "${work}"; }
trap cleanup EXIT

echo "verify-review-ledger: the gate must reject unre-checkable rows, not just pass"

# --- the committed ledger passes --------------------------------------------
if ! node "${gate}" "${ledger}" >/dev/null 2>&1; then
  echo "FAIL: the gate does not pass on the committed ledger" >&2
  node "${gate}" "${ledger}" >&2 || true
  exit 1
fi
echo "  passes: the committed ledger"

# An unmodified copy must also pass, or the cases below prove nothing about the
# planting rather than about the copy.
cp "${ledger}" "${work}"
if ! node "${gate}" "${work}" >/dev/null 2>&1; then
  echo "FAIL: an unmodified copy of the ledger does not pass" >&2
  exit 1
fi
echo "  passes: an unmodified copy"

reject() {
  local label="$1"
  if node "${gate}" "${work}" >/dev/null 2>&1; then
    echo "FAIL: the gate accepted ${label}" >&2
    exit 1
  fi
  echo "  rejects: ${label}"
}

# --- a digest that is not 64 hex --------------------------------------------
cp "${ledger}" "${work}"
awk -F'\t' 'BEGIN{OFS="\t"} NR==2{$3="not-a-sha"} {print}' "${ledger}" > "${work}"
reject "a snapshot_sha256 that is not 64 hex characters"

# --- a topic that resolves to nothing ---------------------------------------
awk -F'\t' 'BEGIN{OFS="\t"} NR==2{$4="no-such-note.md"} {print}' "${ledger}" > "${work}"
reject "a topic that resolves to no file (orphaned evidence)"

# --- a GitHub reference pinned to a short sha -------------------------------
# The row stays otherwise valid; only the coordinate stops being fetchable.
awk -F'\t' 'BEGIN{OFS="\t"}
  $1 ~ /github\.com\/[^\/]+\/[^\/]+\/(blob|tree)\/[0-9a-f]{40}/ && !done {
    sub(/\/(blob|tree)\/[0-9a-f]{40}/, "&", $1)
    gsub(/([0-9a-f]{7})[0-9a-f]{33}/, "\\1", $1)
    done = 1
  }
  {print}' "${ledger}" > "${work}"
reject "a GitHub reference pinned to a short sha"

# --- a duplicated (url, topic) pair -----------------------------------------
{ cat "${ledger}"; awk 'NR==2' "${ledger}"; } > "${work}"
reject "a duplicated (url, topic) pair"

# --- a row with the wrong field count ---------------------------------------
awk -F'\t' 'BEGIN{OFS="\t"} NR==2{print $1,$2,$3,$4; next} {print}' "${ledger}" > "${work}"
reject "a row with four fields instead of five"

# --- a renamed header column ------------------------------------------------
awk -F'\t' 'BEGIN{OFS="\t"} NR==1{$3="sha"} {print}' "${ledger}" > "${work}"
reject "a renamed header column"

# --- an empty disposition ---------------------------------------------------
awk -F'\t' 'BEGIN{OFS="\t"} NR==2{$5=""} {print}' "${ledger}" > "${work}"
reject "a row that records no disposition"

# --- restored, the real ledger still passes ---------------------------------
if ! node "${gate}" "${ledger}" >/dev/null 2>&1; then
  echo "FAIL: the committed ledger stopped passing" >&2
  exit 1
fi
echo "  passes: the committed ledger, unchanged throughout"

echo "verify-review-ledger: all cases behaved"

#!/usr/bin/env bash
# Proves the console-contract gate actually fails on drift.
#
# The gate reads the Go source and the console tables and compares their value
# sets. A gate that only ever passes is indistinguishable from no gate, and this
# one exists precisely because a browser suite over agreeable fixtures passed
# while the tables were wrong. So this plants each kind of drift -- an invented
# console value, and a dropped real one -- and confirms the gate rejects both,
# then restores and confirms the real tree passes.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"

gate="scripts/verify-console-contract.mjs"
subject="web/src/components/statusTags.tsx"

if ! git diff --quiet -- "${subject}" || ! git diff --cached --quiet -- "${subject}"; then
  echo "verify-console-contract self-test: SKIP (${subject} has uncommitted changes)" >&2
  exit 0
fi

restore() { git checkout -- "${subject}"; }
trap restore EXIT

echo "verify-console-contract: the gate must reject drift, not just pass"

# --- a clean tree passes ----------------------------------------------------
if ! node "${gate}" >/dev/null 2>&1; then
  echo "FAIL: the gate does not pass on the committed tree" >&2
  node "${gate}" >&2 || true
  exit 1
fi
echo "  passes: the committed tree"

# --- an invented console value is rejected ----------------------------------
# Add a value the Hub never emits to the rollout map.
restore
perl -0pi -e 's/(export const ROLLOUT_INTENT: Record<string, Intent> = \{\n)/$1  NOT_A_REAL_STATUS: "danger",\n/' "${subject}"
if node "${gate}" >/dev/null 2>&1; then
  echo "FAIL: the gate accepted an invented console value (NOT_A_REAL_STATUS)" >&2
  exit 1
fi
echo "  rejects: an invented console value"

# --- a dropped real value is rejected ---------------------------------------
# Remove a real value (BLOCKED) the Hub emits; the console would render it neutral.
restore
perl -0pi -e 's/\n\s*BLOCKED: "danger",//' "${subject}"
if node "${gate}" >/dev/null 2>&1; then
  echo "FAIL: the gate accepted a console table missing a real Hub value (BLOCKED)" >&2
  exit 1
fi
echo "  rejects: a dropped real value"

# --- restored, it passes again ----------------------------------------------
restore
if ! node "${gate}" >/dev/null 2>&1; then
  echo "FAIL: the gate did not pass after the file was restored" >&2
  exit 1
fi
echo "  passes: the restored tree"

echo "verify-console-contract: all cases behaved"

#!/usr/bin/env bash
# Proves the design-token gate actually fails on drift.
#
# The gate compares the console's Blueprint tokens against the values a live
# Foundry tenant serves. It currently reports 205 of 205 matching, which is the
# answer that makes a gate look unnecessary -- so this plants each kind of drift
# and confirms it is rejected.
#
# The normalizer is the part worth testing hardest. It folds the two builds'
# notation (#fff vs #ffffff, .1s vs 100ms, #0000001a vs rgba(0, 0, 0, 0.1)),
# and a normalizer that folded too much would turn the whole gate into a
# formality. So this also plants a value that differs by one hex digit -- the
# smallest real difference there is -- and requires the gate to catch it.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"

gate="scripts/verify-design-tokens.mjs"
subject="docs/reference-platform/contracts/reference-design-tokens.json"

if ! git diff --quiet -- "${subject}" || ! git diff --cached --quiet -- "${subject}"; then
  echo "verify-design-tokens self-test: SKIP (${subject} has uncommitted changes)" >&2
  exit 0
fi

restore() { git checkout -- "${subject}"; }
trap restore EXIT

# Rewrite one token's value in the captured contract, in place.
plant() {
  node -e '
    const fs = require("node:fs");
    const [file, token, value] = process.argv.slice(1);
    const doc = JSON.parse(fs.readFileSync(file, "utf8"));
    doc.tokens[token] = value;
    fs.writeFileSync(file, JSON.stringify(doc, null, 2) + "\n");
  ' "${subject}" "$1" "$2"
}

drop() {
  node -e '
    const fs = require("node:fs");
    const [file, token] = process.argv.slice(1);
    const doc = JSON.parse(fs.readFileSync(file, "utf8"));
    delete doc.tokens[token];
    fs.writeFileSync(file, JSON.stringify(doc, null, 2) + "\n");
  ' "${subject}" "$1"
}

echo "verify-design-tokens: the gate must reject drift, not just pass"
node --test scripts/lib/resolve-design-token-aliases.test.mjs

# --- a clean tree passes ----------------------------------------------------
if ! node "${gate}" >/dev/null 2>&1; then
  echo "FAIL: the gate does not pass on the committed tree" >&2
  node "${gate}" >&2 || true
  exit 1
fi
echo "  passes: the committed tree"

# --- one hex digit is a real difference -------------------------------------
restore
plant "--bp-intent-primary-rest" "#2d72d3"
if node "${gate}" >/dev/null 2>&1; then
  echo "FAIL: the gate accepted a primary intent off by one hex digit" >&2
  exit 1
fi
echo "  rejects: a colour that differs by one hex digit"

# --- a token the installed Blueprint does not define ------------------------
restore
plant "--bp-invented-token" "#000000"
if node "${gate}" >/dev/null 2>&1; then
  echo "FAIL: the gate accepted a captured token the console cannot supply" >&2
  exit 1
fi
echo "  rejects: a token the console does not define"

# --- a token dropped from the contract --------------------------------------
restore
drop "--bp-palette-blue-3"
if node "${gate}" >/dev/null 2>&1; then
  echo "FAIL: the gate accepted a contract missing a token the console defines" >&2
  exit 1
fi
echo "  rejects: a token dropped from the captured contract"

# --- notation alone is NOT drift --------------------------------------------
# The counter-test. If any of these fail the gate, it is crying wolf on the
# minifier and will be switched off, so they matter as much as the rejections.
restore
plant "--bp-palette-white" "#FFF"
if ! node "${gate}" >/dev/null 2>&1; then
  echo "FAIL: the gate called #FFF a difference from #ffffff" >&2
  node "${gate}" >&2 || true
  exit 1
fi
restore
plant "--bp-emphasis-transition-duration" "100ms"
if ! node "${gate}" >/dev/null 2>&1; then
  echo "FAIL: the gate called 100ms a difference from .1s" >&2
  node "${gate}" >&2 || true
  exit 1
fi
echo "  accepts: the same value in the other build's notation"

# --- restored, it passes again ----------------------------------------------
restore
if ! node "${gate}" >/dev/null 2>&1; then
  echo "FAIL: the gate did not pass after the contract was restored" >&2
  exit 1
fi
echo "  passes: the restored tree"

echo "verify-design-tokens: all cases behaved"

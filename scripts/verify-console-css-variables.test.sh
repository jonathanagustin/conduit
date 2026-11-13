#!/usr/bin/env bash
# Proves the CSS-variable gate rejects the bug class it was written for.
#
# That class is quiet by nature: `var(--wrong-name, right-value)` renders
# correctly, so the gate passing on a clean tree says very little on its own.
# This plants each shape the console actually shipped -- a Blueprint 4/5 name, a
# near-miss for a real token, and a name nobody ever defined -- and requires
# each to fail. Then it plants the two shapes that must NOT fail: a variable a
# component sets as an inline style, and a variable named inside a comment,
# because both exist in the tree and a gate that flagged them would be turned
# off within a day.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"

gate="scripts/verify-console-css-variables.mjs"
subject="web/src/styles/index.css"
component="web/src/components/assistant-ui/shell.tsx"

for file in "${subject}" "${component}"; do
  if ! git diff --quiet -- "${file}" || ! git diff --cached --quiet -- "${file}"; then
    echo "verify-console-css-variables self-test: SKIP (${file} has uncommitted changes)" >&2
    exit 0
  fi
done

restore() { git checkout -- "${subject}" "${component}"; }
trap restore EXIT

plant() { printf '\n.console-self-test { %s }\n' "$1" >> "${subject}"; }

reject() {
  if node "${gate}" >/dev/null 2>&1; then
    echo "FAIL: the gate accepted ${1}" >&2
    exit 1
  fi
  echo "  rejects: ${1}"
  restore
}

accept() {
  if ! node "${gate}" >/dev/null 2>&1; then
    echo "FAIL: the gate rejected ${1}, which is legitimate" >&2
    node "${gate}" >&2 || true
    exit 1
  fi
  echo "  accepts: ${1}"
  restore
}

echo "verify-console-css-variables: the gate must reject dead names, not just pass"

if ! node "${gate}" >/dev/null 2>&1; then
  echo "FAIL: the gate does not pass on the committed tree" >&2
  node "${gate}" >&2 || true
  exit 1
fi
echo "  passes: the committed tree"

# The three shapes this repository actually shipped.
plant 'font-family: var(--pt-font-family-monospace, monospace);'
reject "a Blueprint 4/5 token name Blueprint 6 does not define"

plant 'color: var(--bp-text-color-muted, #5f6b7c);'
reject "a near-miss for a real token, with the right colour as its fallback"

plant 'color: var(--brand);'
reject "a name nothing has ever defined"

# A fallback is not a defence: that is the whole point.
plant 'background: var(--surface-0, var(--bp-palette-light-gray-5, #f6f7f9));'
reject "a dead name whose nested fallback happens to be correct"

# And the two legitimate shapes.
plant 'color: var(--console-self-test-inline);'
perl -0pi -e 's/(import[^\n]*\n)/$1\n\/\/ self-test fixture\nconst SELF_TEST = { "--console-self-test-inline": "#000" };\nvoid SELF_TEST;\n/' "${component}"
accept "a variable a component sets as an inline style"

plant 'color: var(--bp-palette-white); /* mentions var(--never-defined) in prose */'
accept "a dead name appearing only inside a comment"

if ! node "${gate}" >/dev/null 2>&1; then
  echo "FAIL: the gate did not pass after the tree was restored" >&2
  exit 1
fi
echo "  passes: the restored tree"

echo "verify-console-css-variables: all cases behaved"

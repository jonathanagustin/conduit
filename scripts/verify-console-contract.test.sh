#!/usr/bin/env bash
# Proves the console-contract gate actually fails on drift.
#
# The gate compares the bootstrap schema version and meta name in Go with the
# same strings in the assistant. A gate that only ever passes is indistinguishable
# from no gate. This plants each string the assistant reads and confirms the
# gate rejects the disagreement, then restores and confirms the real tree passes.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"

gate="scripts/verify-console-contract.mjs"
subject="web/src/runtime/bootstrap.ts"

if ! git diff --quiet -- "${subject}" || ! git diff --cached --quiet -- "${subject}"; then
  echo "verify-console-contract self-test: SKIP (${subject} has uncommitted changes)" >&2
  exit 0
fi

restore() { git checkout -- "${subject}"; }
trap restore EXIT

echo "verify-console-contract: the gate must reject drift, not just pass"

if ! node "${gate}" >/dev/null 2>&1; then
  echo "FAIL: the gate does not pass on the committed tree" >&2
  node "${gate}" >&2 || true
  exit 1
fi
echo "  passes: the committed tree"

restore
perl -0pi -e 's/conduit\.console\.bootstrap\/v1/conduit.console.bootstrap\/v9/' "${subject}"
if node "${gate}" >/dev/null 2>&1; then
  echo "FAIL: the gate accepted an invented bootstrap schema version" >&2
  exit 1
fi
echo "  rejects: an invented schema version"

restore
perl -0pi -e 's/x-conduit-console-bootstrap/x-conduit-console-other/' "${subject}"
if node "${gate}" >/dev/null 2>&1; then
  echo "FAIL: the gate accepted a renamed bootstrap meta" >&2
  exit 1
fi
echo "  rejects: a renamed meta"

restore
if ! node "${gate}" >/dev/null 2>&1; then
  echo "FAIL: the gate did not pass after the file was restored" >&2
  exit 1
fi
echo "  passes: the restored tree"

echo "verify-console-contract: all cases behaved"

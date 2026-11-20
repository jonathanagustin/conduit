#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."
: "${ZARF_BIN:?set ZARF_BIN to the verified Zarf v1.0.1 binary}"
: "${UDS_BIN:?set UDS_BIN to the verified UDS v0.41.0 binary}"
export CONDUIT_TEST_UDS_EXECUTOR_CLI=1
export GOMAXPROCS="${GOMAXPROCS:-2}"
"${NODE_BIN:-node}" --test scripts/verify-uds-cli-evidence.test.mjs
work=$(mktemp -d)
cleanup() {
  "${NODE_BIN:-node}" -e 'require("node:fs").rmSync(process.argv[1], {recursive: true, force: true})' "$work"
}
trap cleanup EXIT
if ! "${GO_BIN:-go}" test -race -p 2 -count=1 -json ./internal/agent/adapters/uds > "$work/tests.jsonl" 2>&1; then
  cat "$work/tests.jsonl" >&2
  exit 1
fi
# Some emulated race runtimes can exit zero before TestMain. A package-level
# success is insufficient: require the actual positive and negative proofs.
"${NODE_BIN:-node}" scripts/verify-uds-cli-evidence.mjs proof "$work/tests.jsonl"

#!/usr/bin/env bash
# Proves verify-documentation-freshness.mjs rejects, not just that it accepts.
#
# A freshness gate is unusually easy to write so that it can never fail: read a
# date, compare it to a threshold, and if either side is wrong the comparison
# quietly always passes. Each case below plants exactly one defect into a
# throwaway copy of the tree and requires a non-zero exit.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "${HERE}/.." && pwd)"
STAGE="$(mktemp -d)"
trap 'rm -rf "${STAGE}"' EXIT

mkdir -p "${STAGE}/scripts" "${STAGE}/docs/apollo"
cp "${ROOT}/scripts/verify-documentation-freshness.mjs" "${STAGE}/scripts/"
cp "${ROOT}/docs/apollo/public-documentation-capture.json" "${STAGE}/docs/apollo/"
cp "${ROOT}/docs/apollo/public-documentation-manifest.tsv" "${STAGE}/docs/apollo/"

CAPTURE="${STAGE}/docs/apollo/public-documentation-capture.json"
MANIFEST="${STAGE}/docs/apollo/public-documentation-manifest.tsv"
PRISTINE="$(cat "${CAPTURE}")"
PRISTINE_MANIFEST="$(cat "${MANIFEST}")"

failures=0

restore() {
  printf '%s' "${PRISTINE}" > "${CAPTURE}"
  printf '%s' "${PRISTINE_MANIFEST}" > "${MANIFEST}"
}

expect_reject() {
  local name="$1" today="${2:-}"
  if CONDUIT_DOCUMENTATION_TODAY="${today}" node "${STAGE}/scripts/verify-documentation-freshness.mjs" >/dev/null 2>&1; then
    echo "SURVIVED  ${name}"
    failures=$((failures + 1))
  else
    echo "rejected  ${name}"
  fi
  restore
}

# The gate must accept the tree as committed, or every rejection below proves
# nothing -- this is the check that the harness is testing the real thing.
if ! node "${STAGE}/scripts/verify-documentation-freshness.mjs" >/dev/null 2>&1; then
  echo "FAIL the unmodified tree is already rejected; every case below is meaningless"
  exit 1
fi
echo "accepted  the committed tree"

expect_reject "a corpus older than the maximum age" "2026-12-31"

node -e '
const path = process.argv[1];
const capture = JSON.parse(require("fs").readFileSync(path, "utf8"));
capture.pageCount = capture.pageCount + 1;
require("fs").writeFileSync(path, JSON.stringify(capture, null, 2));
' "${CAPTURE}"
expect_reject "a page count that disagrees with the manifest"

head -n -1 "${MANIFEST}" > "${MANIFEST}.trimmed" && mv "${MANIFEST}.trimmed" "${MANIFEST}"
expect_reject "a manifest edited without re-recording the capture"

node -e '
const path = process.argv[1];
const capture = JSON.parse(require("fs").readFileSync(path, "utf8"));
capture.capturedAt = "2027-01-01";
require("fs").writeFileSync(path, JSON.stringify(capture, null, 2));
' "${CAPTURE}"
expect_reject "a capture dated in the future" "2026-08-15"

node -e '
const path = process.argv[1];
const capture = JSON.parse(require("fs").readFileSync(path, "utf8"));
capture.capturedAt = "recently";
require("fs").writeFileSync(path, JSON.stringify(capture, null, 2));
' "${CAPTURE}"
expect_reject "a capturedAt that is not a date"

node -e '
const path = process.argv[1];
const capture = JSON.parse(require("fs").readFileSync(path, "utf8"));
delete capture.maximumAgeDays;
require("fs").writeFileSync(path, JSON.stringify(capture, null, 2));
' "${CAPTURE}"
expect_reject "a capture record with no maximum age"

if [ "${failures}" -ne 0 ]; then
  echo "FAIL ${failures} planted violation(s) survived"
  exit 1
fi
echo "PASS documentation freshness gate rejects every planted violation"

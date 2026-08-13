#!/usr/bin/env bash
# Plant one mutant, run the package's tests, and report whether the suite caught
# it. Restores the file afterwards.
#
#   scripts/verify-mutant-killed.sh internal/controlplane/bundles.go 1360 \
#     'compareVersions(a, b) >= 0' 'compareVersions(a, b) > 0'
#
# This exists because the obvious inline version of it destroyed work three
# times in one session. The restore is `git checkout -- <file>`, which cannot
# tell your uncommitted edit from the planted mutant, and discards both without
# saying so. The rule that follows -- commit before you plant -- is easy to state
# and easy to forget at the moment it matters, so the script enforces it rather
# than trusting the operator to remember.
#
# It also refuses to restore anything but the file it mutated. An earlier
# version restored the test file alongside the source, which wiped the test
# being written and made the next run report SURVIVED against a suite that no
# longer contained the assertion.
set -euo pipefail

file="${1:-}"
line="${2:-}"
from="${3:-}"
to="${4:-}"
if [[ -z "${file}" || -z "${line}" || -z "${from}" ]]; then
  echo "usage: $0 <file> <line> <from> <to>" >&2
  echo "  <to> may be empty to delete the matched text." >&2
  exit 2
fi

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"

if [[ ! -f "${file}" ]]; then
  echo "no such file: ${file}" >&2
  exit 2
fi

# The guard this script exists for.
if ! git diff --quiet -- "${file}" || ! git diff --cached --quiet -- "${file}"; then
  cat >&2 <<MESSAGE
refusing to plant a mutant in ${file}: it has uncommitted changes.

The kill check restores the file with \`git checkout\`, which would discard your
work along with the mutant, silently. Commit or stash first.
MESSAGE
  exit 1
fi

package="./$(dirname "${file}")/"
restore() { git checkout -- "${file}"; }
trap restore EXIT

python3 - "${file}" "${line}" "${from}" "${to}" <<'PYTHON'
import sys

path, line, old, new = sys.argv[1], int(sys.argv[2]), sys.argv[3], sys.argv[4]
lines = open(path).read().split("\n")
index = line - 1
if old not in lines[index]:
    sys.exit(f"line {line} does not contain {old!r}:\n  {lines[index]}")
lines[index] = lines[index].replace(old, new, 1)
open(path, "w").write("\n".join(lines))
PYTHON

# A mutant that hangs is not a mutant that survives, and the difference matters:
# a timed-out mutant is excluded from an efficacy ratio rather than counted
# against it, so a hang hides inside a healthy-looking number. Report it as its
# own outcome.
if timeout "${MUTANT_TIMEOUT_SECONDS:-180}" go test "${package}" -count=1 >/dev/null 2>&1; then
  echo "SURVIVED  ${file}:${line}  ${from} -> ${to:-(deleted)}"
  exit 0
fi
status=$?
if [[ "${status}" -eq 124 ]]; then
  echo "HUNG      ${file}:${line}  ${from} -> ${to:-(deleted)}  (did not terminate)"
  exit 0
fi
echo "caught    ${file}:${line}  ${from} -> ${to:-(deleted)}"

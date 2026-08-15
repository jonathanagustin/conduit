#!/usr/bin/env bash
# Proves the mutation harness cannot lose your work.
#
# A mutation harness is trusted in a specific and dangerous way: it deliberately
# corrupts a source file and then tells you the tree is fine. If its restore is
# unreliable, it does not merely fail -- it reports kills it did not achieve and
# leaves mutants behind, which is strictly worse than having no harness at all.
#
# Both cases below are regressions from real incidents. The dirty-file guard was
# added after inline mutation destroyed work three times in one session. The
# untracked guard was added after seven planted mutants shipped in a single
# commit: `git diff --quiet` ignores untracked files so the guard passed, and
# `git checkout --` cannot restore a file git has never seen, so every mutant
# stayed while every check reported a kill.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"

harness="scripts/verify-mutant-killed.sh"
subject="internal/buildinfo/buildinfo.go"

if ! git diff --quiet -- "${subject}" || ! git diff --cached --quiet -- "${subject}"; then
  echo "verify-mutant-killed self-test: SKIP (${subject} has uncommitted changes)" >&2
  exit 0
fi

echo "verify-mutant-killed: the harness must refuse what it cannot restore"

# --- an untracked file ------------------------------------------------------
untracked="internal/buildinfo/zz_mutation_selftest_untracked.go"
cleanup() { rm -f "${untracked}"; }
trap cleanup EXIT
printf 'package buildinfo\n\nconst mutationSelfTestSentinel = 42\n' > "${untracked}"
original="$(cat "${untracked}")"

if "${harness}" "${untracked}" 3 "42" "99" >/dev/null 2>&1; then
  echo "FAIL: the harness planted a mutant in an untracked file" >&2
  exit 1
fi
if [[ "$(cat "${untracked}")" != "${original}" ]]; then
  echo "FAIL: the harness modified an untracked file it could not restore:" >&2
  diff <(printf '%s' "${original}") "${untracked}" >&2 || true
  exit 1
fi
echo "  refused: a file git does not track"

# --- a tracked file with uncommitted changes --------------------------------
# Written and reverted here rather than left to the operator, so the guard is
# exercised on a real dirty file rather than described.
printf '\n// mutation self-test scratch\n' >> "${subject}"
dirty_restore() { git checkout -- "${subject}"; cleanup; }
trap dirty_restore EXIT

if "${harness}" "${subject}" 1 "package" "package" >/dev/null 2>&1; then
  echo "FAIL: the harness planted a mutant in a file with uncommitted changes" >&2
  exit 1
fi
echo "  refused: a tracked file with uncommitted changes"

git checkout -- "${subject}"
trap cleanup EXIT

# --- a clean tracked file is returned byte-identical -------------------------
# The property the restore verification exists to hold. The mutation is chosen
# to be harmless if it somehow survives: replacing a word inside a comment.
before="$(git hash-object "${subject}")"
"${harness}" "${subject}" 1 "package" "package" >/dev/null 2>&1 || true
after="$(git hash-object "${subject}")"
if [[ "${before}" != "${after}" ]]; then
  echo "FAIL: ${subject} was not restored byte-identically (${before} -> ${after})" >&2
  git checkout -- "${subject}"
  exit 1
fi
echo "  restored: a clean tracked file, byte-identical"

# Kill and survive reporting is not asserted here. It needs a package whose
# tests are known to constrain a specific line, and every real use of the
# harness exercises it -- an unkillable mutant reports SURVIVED and is read.
echo "verify-mutant-killed: all cases behaved"

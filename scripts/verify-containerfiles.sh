#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
hadolint_bin="${HADOLINT_BIN:-hadolint}"

# Every Containerfile in the repository, not a hand-maintained pair.
#
# The list was `Containerfile` and `Containerfile.agent` only, so the two
# optimizer Containerfiles — the largest and the ones doing the most unusual
# build work — were never linted, and adding a violation to either left this
# gate green. Both pass today, so widening the gate costs nothing and closes
# the hole. Globbed rather than listed: a Containerfile added tomorrow is
# covered without anyone remembering to add it here.
shopt -s nullglob
containerfiles=("${repo_root}"/Containerfile*)
shopt -u nullglob
if [[ "${#containerfiles[@]}" -eq 0 ]]; then
  echo "no Containerfiles found in ${repo_root}; the glob no longer matches what it guards" >&2
  exit 1
fi

"${hadolint_bin}" --failure-threshold info "${containerfiles[@]}"

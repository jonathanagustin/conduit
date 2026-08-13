#!/usr/bin/env bash
# Run mutation testing over one package and report the surviving mutants.
#
# This is a review tool, not a gate. A full pass over internal/controlplane is
# roughly four hours: mutation testing recompiles the package under test once
# per mutant, so the package itself is always a cache miss no matter how warm
# the build cache is. Blocking every pull request on that would be indefensible.
# It earns its keep run deliberately against a package you are changing.
#
#   scripts/run-mutation-testing.sh ./internal/agent
#   scripts/run-mutation-testing.sh ./internal/controlplane --dry-run
#
# What survives is the point. A LIVED mutant is a line the tests execute and do
# not constrain: change it and nothing fails. Some are equivalent mutants that
# cannot be killed -- > against >= where a loop takes a maximum, < against <= in
# a comparator over unique keys -- so read the list rather than chasing the
# percentage. Efficacy of 100% is a reason to check the tests are asserting
# something, not a target.
set -euo pipefail

# Pinned like every other tool this repository downloads. gremlins reports its
# own version as "dev" when built from source, so the pin lives here.
GREMLINS_VERSION="${GREMLINS_VERSION:-v0.6.0}"

# gremlins derives each mutant's test timeout by multiplying a baseline run, so
# this coefficient is a multiplier and not a duration. Both ends hurt:
#
#   Too low and healthy mutants are reported as TIMED OUT. A first attempt at
#   the tool default reported all 42 mutants of internal/grypescanner as timed
#   out and 0.00% efficacy, which reads as catastrophic and is entirely an
#   artifact.
#
#   Too high and a mutant that makes a test hang -- rather than fail -- blocks
#   for baseline x coefficient before it is killed. internal/agent's suite
#   takes about four seconds, so a coefficient of 200 gives a hanging mutant
#   thirteen minutes of wall clock each. That stalled a run so thoroughly it
#   looked like a deadlock.
#
# Timed-out mutants are excluded from the efficacy ratio rather than counted as
# survivors, so an over-tight timeout reports a HIGHER efficacy than the truth.
# Read the TIMED OUT count before believing the percentage.
#
# The multiplicand is NOT a test run. gremlins takes its baseline from the
# coverage pass, which for internal/controlplane is about 1.06s -- while a
# mutant that gets KILLED takes about 6.67s, because failing tests do more work
# than passing ones, and the clean suite itself takes 1.4s. So the honest
# multiplier is against ~1s for a ~7s worst case, and the tool default is
# nowhere near it. Measured on internal/controlplane: coefficient 4 timed out
# 18 of the first 26 mutants, 10 still timed out 17%, and 100 timed out none.
#
# Combined with the exclusion above, that is the whole trap: the run that looks
# best is the one measuring least.
#
# 20 suits the sub-second packages. Raise it for any package whose suite runs
# for a second or more -- the product is what matters, not this number -- and
# check the TIMED OUT count is zero before reading anything else.
TIMEOUT_COEFFICIENT="${TIMEOUT_COEFFICIENT:-20}"

# Leave a core for the machine; mutation testing will otherwise starve the
# editor and the test binaries it is spawning. Note that the baseline is
# measured once at startup, so a machine that becomes loaded afterwards pushes
# every later mutant towards a timeout it would not otherwise hit.
WORKERS="${WORKERS:-$(( $(nproc 2>/dev/null || echo 4) - 1 ))}"

package="${1:-}"
if [[ -z "${package}" ]]; then
  echo "usage: $0 <package> [gremlins flags...]" >&2
  echo "example: $0 ./internal/agent" >&2
  exit 2
fi
shift

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"

if command -v gremlins >/dev/null 2>&1; then
  gremlins_bin="gremlins"
else
  echo "installing gremlins ${GREMLINS_VERSION}" >&2
  go install "github.com/go-gremlins/gremlins/cmd/gremlins@${GREMLINS_VERSION}"
  gremlins_bin="$(go env GOPATH)/bin/gremlins"
fi

"${gremlins_bin}" unleash \
  --timeout-coefficient "${TIMEOUT_COEFFICIENT}" \
  --workers "${WORKERS}" \
  "$@" \
  "${package}"

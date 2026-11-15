#!/usr/bin/env bash
# Measure the mutation efficacy of the bounded-context Go domains and the web
# console, and ratchet it: an entry may only get better, never worse.
#
# Why this and not the one-mutant harness: scripts/verify-mutant-killed.sh proves
# a specific claim -- this gate rejects that mutation -- one hand-planted mutant
# at a time. This measures the suite as a whole, over mutants nobody thought to
# plant, and produces a number that can regress and be caught.
#
# The score is EFFICACY: killed / (killed + lived). A survivor is a LIVED mutant
# -- a change to the code that no test noticed. Coverage being 100% is not
# enough; a line can be run by a test that would not fail if the line were wrong.
#
# Two facts about running gremlins on this repo are encoded here because each one
# cost time to find:
#   1. A mutation run rebuilds a subpackage and can drop an unused-in-that-scope
#      dependency from go.mod (gqlparser, used by the Apollo interface, is the
#      one that bit). So go.mod and go.sum are restored after every package.
#   2. This repo's unit suites run in milliseconds, so a tight per-mutant timeout
#      makes a merely-slow mutant TIME OUT -- and a timed-out mutant is not
#      counted as a survivor, which INFLATES efficacy. The timeout coefficient in
#      .gremlins.yaml is set high enough that survivors surface as LIVED rather
#      than hide as TIMED OUT. Do not lower it to make a sweep faster.
#
# Usage:
#   scripts/mutation-sweep.sh            # measure, ratchet, fail on regression
#   scripts/mutation-sweep.sh --write    # also raise the baseline where improved
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"

scores_file="docs/operations/mutation-scores.json"
write=false
[[ "${1:-}" == "--write" ]] && write=true

# The survivor (LIVED) count is deterministic and is the real signal. Efficacy
# jitters run to run by about a point because a merely-slow mutant lands as
# KILLED on one machine and TIMED OUT on another -- and a timed-out mutant is
# excluded from the efficacy denominator rather than counted, so the same code
# scores slightly differently on a loaded runner. The regression check is given
# this much slack so ordinary jitter does not read as a real drop; a genuine
# new survivor moves efficacy far more than this and still fails.
tolerance=2.0
floor_with_tolerance() { awk -v f="$1" -v t="${tolerance}" 'BEGIN{v=f-t; print (v<0?0:v)}'; }

command -v gremlins >/dev/null || {
  echo "gremlins is required: go install github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0" >&2
  exit 1
}
command -v pnpm >/dev/null || {
  echo "pnpm is required for the web Stryker sweep; install the pinned pnpm as CI does" >&2
  exit 1
}

# The bounded-context domain packages: the rules, where a surviving mutant is
# most alarming and least excusable. internal/controlplane is deliberately not
# swept here -- it is the largest and least-changing target, and the domains are
# where the meaning lives.
packages=(
  internal/environments/domain
  internal/governance/domain
  internal/catalog/domain
  internal/apollographql/domain
  internal/reporting/domain
)

baseline_efficacy() {
  local section="$1" key="$2"
  [[ -f "${scores_file}" ]] || { echo "0"; return; }
  jq -r --arg s "${section}" --arg k "${key}" '.[$s][$k].nonEquivalentEfficacy // .[$s][$k].efficacy // 0' "${scores_file}"
}

restore_manifest() { git checkout -- go.mod go.sum 2>/dev/null || true; }
trap restore_manifest EXIT

tmp="$(mktemp -d)"
results="${tmp}/results.jsonl"
web_results="${tmp}/web.jsonl"
: > "${results}"
: > "${web_results}"
regressed=0

for pkg in "${packages[@]}"; do
  floor="$(baseline_efficacy packages "${pkg}")"
  threshold="$(floor_with_tolerance "${floor}")"
  echo "sweeping ${pkg} (baseline efficacy ${floor}%, fails below ${threshold}%)..."
  out="${tmp}/$(echo "${pkg}" | tr / _).json"
  # Request an ungated raw engine report, then enforce the existing floor over
  # non-equivalent mutations ourselves. Preserve raw counts and efficacy.
  # Capture engine errors without aborting the other measurements.
  set +e
  gremlins unleash --output "${out}" --threshold-efficacy 0 "./${pkg}"
  code=$?
  set -e
  restore_manifest

  row="$(node scripts/score-mutation-report.mjs "${pkg}" "${out}")"
  printf '%s\n' "${row}" >> "${results}"
  efficacy="$(jq -r '.efficacy' <<<"${row}")"
  measured_quality="$(jq -r '.nonEquivalentEfficacy' <<<"${row}")"
  equivalents="$(jq -r '.equivalent' <<<"${row}")"
  echo "  ${pkg}: raw efficacy ${efficacy}%; ${equivalents} reviewed equivalents; non-equivalent efficacy ${measured_quality}% (floor ${threshold}%)"
  # The runner owns the quality decision; the engine exit still catches errors.
  if [[ ${code} -ne 0 ]] || ! awk -v actual="${measured_quality}" -v floor="${threshold}" 'BEGIN{exit !(actual + 0.0001 >= floor)}'; then
    regressed=1
  fi
done

# --- the web console, measured by Stryker --------------------------------
# Stryker is already configured (web/stryker.config.json) but its break
# threshold is null, so `stryker run` never failed anything -- the same
# configured-but-unrun surface this sweep exists to close. We run it, read its
# machine-readable report, and record the same efficacy = killed/(killed+lived)
# per file, where a Survived mutant is the LIVED one and a Timeout counts as
# detected. NoCoverage mutants are excluded from efficacy and shown as the gap
# in coverage, exactly as gremlins' mutator coverage is for Go.
if [[ -f web/stryker.config.json ]]; then
report="web/reports/mutation/mutation.json"
echo "sweeping web console with Stryker..."
set +e
pnpm --dir web exec stryker run --reporters json,clear-text
web_code=$?
set -e
if [[ ${web_code} -ne 0 || ! -f "${report}" ]]; then
  echo "FAIL mutation sweep: Stryker did not produce ${report}" >&2
  exit 1
fi
# Fold the per-mutant statuses into per-file efficacy. node, not jq, because the
# grouping reads far more clearly and node is already a build dependency here.
node -e '
  const fs = require("fs");
  const rep = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
  const out = [];
  for (const [file, data] of Object.entries(rep.files)) {
    const c = {};
    for (const m of data.mutants) c[m.status] = (c[m.status] || 0) + 1;
    const killed = (c.Killed || 0) + (c.Timeout || 0);
    const lived = c.Survived || 0;
    const total = Object.values(c).reduce((a, b) => a + b, 0);
    const covered = killed + lived;
    const efficacy = covered === 0 ? 100 : (100 * killed) / covered;
    const coverage = total === 0 ? 0 : (100 * covered) / total;
    out.push(JSON.stringify({ file: "web/" + file, efficacy, killed, lived, total, coverage }));
  }
  process.stdout.write(out.join("\n") + "\n");
' "${report}" > "${web_results}"
while IFS= read -r row; do
  [[ -z "${row}" ]] && continue
  file="$(jq -r '.file' <<<"${row}")"
  eff="$(jq -r '.efficacy' <<<"${row}")"
  killed="$(jq -r '.killed' <<<"${row}")"
  lived="$(jq -r '.lived' <<<"${row}")"
  floor="$(baseline_efficacy web "${file}")"
  threshold="$(floor_with_tolerance "${floor}")"
  echo "  ${file}: efficacy ${eff}% (${killed} killed, ${lived} lived; baseline ${floor}%, fails below ${threshold}%)"
  # awk for the float comparison; a regression is efficacy below the tolerated floor.
  awk -v a="${eff}" -v b="${threshold}" 'BEGIN{exit !(a + 0.0001 < b)}' && regressed=1
done < "${web_results}"

else
  if [[ "$(jq '.web | length' "${scores_file}")" != 0 ]]; then
    echo 'FAIL mutation sweep: recorded frontend targets exist but their Stryker configuration is missing' >&2
    exit 1
  fi
  echo 'No configured frontend mutation targets; frontend efficacy is not measured.'
fi

if [[ ${regressed} -ne 0 ]]; then
  echo "FAIL mutation sweep: an entry regressed below its recorded efficacy" >&2
  exit 1
fi

if [[ "${write}" == true ]]; then
  captured_at="$(date -u +%Y-%m-%d)"
  jq -n --slurpfile rows "${results}" --slurpfile webrows "${web_results}" --arg at "${captured_at}" '
    {
      note: "Mutation efficacy baseline from scripts/mutation-sweep.sh. `packages` is per Go bounded-context domain (gremlins); `web` is per console source file (Stryker). Efficacy is killed/(killed+lived); a survivor is a LIVED mutant no test caught. Ratcheted: an entry may only improve. Regenerate with scripts/mutation-sweep.sh --write after a real sweep.",
      capturedAt: $at,
      packages: ($rows | map({key: .package, value: {efficacy, nonEquivalentEfficacy, equivalent, equivalentManifestSha256, killed, lived, total, coverage}}) | from_entries),
      web: ($webrows | map({key: .file, value: {efficacy, killed, lived, total, coverage}}) | from_entries)
    }' > "${scores_file}"
  echo "wrote ${scores_file} (${captured_at})"
fi

echo "PASS mutation sweep"

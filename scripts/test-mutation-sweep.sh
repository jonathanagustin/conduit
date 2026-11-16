#!/usr/bin/env bash
# Exercise sweep CLI failure modes without compiling planted Go mutations.
#
# Every sweep invocation runs with pnpm removed from PATH. The candidate lint
# lane does not install pnpm, and an unconfigured frontend sweep must still
# pass there. A failure used to exit 1 with its reason trapped in a temp log,
# so the gate printed nothing after the scoring self-test. Refusals now have
# to name their reason, and any other failure prints the log.
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
fixture=$(mktemp -d)
toolbin=$(mktemp -d)
trap 'rm -rf "$fixture" "$toolbin"' EXIT
mkdir -p "$fixture/scripts" "$fixture/docs/operations" "$fixture/bin"
cp "$root/scripts/mutation-sweep.sh" "$root/scripts/score-mutation-report.mjs" "$root/scripts/mutation-report-score.mjs" "$fixture/scripts/"
cp "$root/docs/operations/mutation-scores.json" "$fixture/docs/operations/original.json"
cat > "$fixture/bin/gremlins" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
output=
while (($#)); do
 case "$1" in
  --output) output=$2;shift 2;;
  *) shift;;
 esac
done
if [[ ${SWEEP_FIXTURE_REGRESSION:-0} == 1 ]]; then
 printf '{"test_efficacy":0,"mutants_killed":0,"mutants_lived":1,"mutants_total":1,"mutations_coverage":100}\n' > "$output"
 exit "${SWEEP_FIXTURE_ENGINE_EXIT:-1}"
fi
printf '{"test_efficacy":100,"mutants_killed":1,"mutants_lived":0,"mutants_total":1,"mutations_coverage":100}\n' > "$output"
STUB
chmod +x "$fixture/bin/gremlins"
# node and jq may share a directory with pnpm. Link the tools the sweep needs
# into a directory that does not contain pnpm, then keep only the standard
# directories that also do not.
for cmd in node jq git; do
  src=$(command -v "$cmd") || { echo "missing ${cmd}" >&2; exit 1; }
  ln -s "$src" "$toolbin/$cmd"
done
sweep_path="$fixture/bin:$toolbin"
IFS=: read -ra path_dirs <<< "$PATH"
for dir in "${path_dirs[@]}"; do
  [[ -z "$dir" || -x "$dir/pnpm" ]] && continue
  sweep_path="$sweep_path:$dir"
done
if PATH="$sweep_path" command -v pnpm >/dev/null 2>&1; then
  echo 'pnpm is still visible to the sweep fixture' >&2
  exit 1
fi
cd "$fixture"
git init -q
touch go.mod go.sum
git add go.mod go.sum
git -c user.name='Conduit test fixture' -c user.email='fixture@example.invalid' commit -qm 'test: initialize sweep fixture'
reset_scores() { cp docs/operations/original.json docs/operations/mutation-scores.json; }
unchanged() { [[ $(sha256sum docs/operations/mutation-scores.json) == "$before" ]] || { echo 'failed sweep rewrote the baseline' >&2; exit 1; }; }
show_log() { echo "sweep log:" >&2; cat "$1" >&2; }
# A refused sweep must fail and say why. Any other outcome prints the log.
expect_refusal() {
  local log=$1 reason=$2
  shift 2
  if PATH="$sweep_path" "$@" >"$log" 2>&1; then
    echo "sweep succeeded; expected refusal: ${reason}" >&2
    show_log "$log"
    exit 1
  fi
  if ! grep -q "$reason" "$log"; then
    echo "sweep failed without the expected reason: ${reason}" >&2
    show_log "$log"
    exit 1
  fi
}
expect_success() {
  local log=$1
  shift
  if ! PATH="$sweep_path" "$@" >"$log" 2>&1; then
    echo 'mutation sweep failed:' >&2
    show_log "$log"
    exit 1
  fi
}
reset_scores
before=$(sha256sum docs/operations/mutation-scores.json)
expect_refusal regression.log 'FAIL mutation sweep: an entry regressed below its recorded efficacy' \
  env SWEEP_FIXTURE_REGRESSION=1 bash scripts/mutation-sweep.sh --write
unchanged
expect_refusal lying-engine.log 'FAIL mutation sweep: an entry regressed below its recorded efficacy' \
  env SWEEP_FIXTURE_REGRESSION=1 SWEEP_FIXTURE_ENGINE_EXIT=0 bash scripts/mutation-sweep.sh --write
unchanged
reset_scores
expect_success success.log bash scripts/mutation-sweep.sh --write
jq -e '(.packages | length) == 5 and all(.packages[]; .efficacy == 100 and .killed == 1 and .lived == 0) and (.web | length) == 0' docs/operations/mutation-scores.json >/dev/null
grep -q 'frontend efficacy is not measured' success.log
reset_scores
jq '.web = {"web/recorded-target.ts": {"efficacy":100,"killed":1,"lived":0,"total":1,"coverage":100}}' docs/operations/mutation-scores.json > with-web.json
mv with-web.json docs/operations/mutation-scores.json
before=$(sha256sum docs/operations/mutation-scores.json)
expect_refusal missing-web.log 'FAIL mutation sweep: recorded frontend targets exist but their Stryker configuration is missing' \
  bash scripts/mutation-sweep.sh --write
unchanged
reset_scores
before=$(sha256sum docs/operations/mutation-scores.json)
mkdir -p web
printf '{}\n' > web/stryker.config.json
expect_refusal missing-pnpm.log 'FAIL mutation sweep: pnpm is required to run the configured Stryker sweep' \
  bash scripts/mutation-sweep.sh --write
unchanged
rm -rf web
echo 'Mutation sweep refuses regressed writes, missing measured targets, and a configured Stryker sweep without pnpm; unmeasured frontend scope remains explicit'

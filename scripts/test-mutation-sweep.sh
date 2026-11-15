#!/usr/bin/env bash
# Exercise sweep CLI failure modes without compiling planted Go mutations.
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
fixture=$(mktemp -d)
trap 'rm -rf "$fixture"' EXIT
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
export PATH="$fixture/bin:$PATH"
cd "$fixture"
git init -q
touch go.mod go.sum
git add go.mod go.sum
git -c user.name='Conduit test fixture' -c user.email='fixture@example.invalid' commit -qm 'test: initialize sweep fixture'
reset_scores() { cp docs/operations/original.json docs/operations/mutation-scores.json; }
unchanged() { [[ $(sha256sum docs/operations/mutation-scores.json) == "$before" ]] || { echo 'failed sweep rewrote the baseline' >&2;exit 1; }; }
reset_scores
before=$(sha256sum docs/operations/mutation-scores.json)
if SWEEP_FIXTURE_REGRESSION=1 bash scripts/mutation-sweep.sh --write > regression.log 2>&1; then
 echo '--write accepted a regressed sweep' >&2;exit 1
fi
unchanged
if SWEEP_FIXTURE_REGRESSION=1 SWEEP_FIXTURE_ENGINE_EXIT=0 bash scripts/mutation-sweep.sh --write > lying-engine.log 2>&1; then
 echo 'sweep trusted a successful engine exit over its regressed measurement' >&2;exit 1
fi
unchanged
reset_scores
bash scripts/mutation-sweep.sh --write > success.log 2>&1
jq -e '(.packages | length) == 5 and all(.packages[]; .efficacy == 100 and .killed == 1 and .lived == 0) and (.web | length) == 0' docs/operations/mutation-scores.json >/dev/null
grep -q 'frontend efficacy is not measured' success.log
reset_scores
jq '.web = {"web/recorded-target.ts": {"efficacy":100,"killed":1,"lived":0,"total":1,"coverage":100}}' docs/operations/mutation-scores.json > with-web.json
mv with-web.json docs/operations/mutation-scores.json
before=$(sha256sum docs/operations/mutation-scores.json)
if bash scripts/mutation-sweep.sh --write > missing-web.log 2>&1; then
 echo 'sweep discarded measured frontend targets with no configuration' >&2;exit 1
fi
unchanged
echo 'Mutation sweep refuses regressed writes and missing measured targets; unmeasured frontend scope remains explicit'

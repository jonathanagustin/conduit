#!/usr/bin/env bash
set -euo pipefail

repo_root="$(git rev-parse --show-toplevel)"
scratch="$(mktemp -d)"
trap 'rm -rf -- "$scratch"' EXIT
mkdir -p "$scratch/bin" "$scratch/evidence"

cat > "$scratch/bin/cursor-agent" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--version" ]]; then
  printf '%s\n' "mock-cursor-agent"
  exit 0
fi
workspace=""
model=""
prompt="${!#}"
while (( $# > 0 )); do
  case "$1" in
    --workspace) workspace="$2"; shift 2 ;;
    --model) model="$2"; shift 2 ;;
    *) shift ;;
  esac
done
[[ -f "$workspace/internal/optimization/evaluator.go" ]]
[[ ! -e "$workspace/.git" ]]
[[ "$prompt" == *"Read files directly from this workspace"* ]]
jq -n --arg model "$model" '{type:"result",subtype:"success",is_error:false,request_id:("request-"+$model),session_id:("session-"+$model),duration_api_ms:1,duration_ms:1,usage:{inputTokens:1,outputTokens:1,cacheReadTokens:0,cacheWriteTokens:0},result:"{\"inputAccessStatus\":\"COMPLETE\",\"findings\":[],\"residualUnknowns\":[]}"}'
EOF
chmod +x "$scratch/bin/cursor-agent"

PATH="$scratch/bin:$PATH" CURSOR_API_KEY="test-only" \
  "$repo_root/scripts/run-evaluator-batch.sh" HEAD "$scratch/evidence" mock-model >/dev/null

jq -e '.secretMatches == 0 and .snapshotId == ("snap_" + .archiveSha256)' "$scratch/evidence/snapshot.json" >/dev/null
jq -e '.model == "mock-model" and .wallDurationMilliseconds >= 0' "$scratch/evidence/mock-model.timing.json" >/dev/null
jq -e '.is_error == false and .usage.inputTokens == 1' "$scratch/evidence/mock-model.receipt.json" >/dev/null
jq -e '.cursorAgentVersion != "" and .runnerRevision != ""' "$scratch/evidence/runner.json" >/dev/null
test -s "$scratch/evidence/prompt.txt"

node "$repo_root/scripts/export-zenith-evaluator-trial.mjs" \
  "$repo_root/docs/operations/evidence/evaluators/2026-08-08-02" \
  "$repo_root/docs/operations/evidence/evaluators/2026-08-08-02/batch-result-v2.json" \
  "$repo_root/docs/operations/evidence/evaluators/2026-08-08-02/zenith-trial.json" \
  --check

node "$repo_root/scripts/export-zenith-evaluator-trial.mjs" \
  "$repo_root/docs/operations/evidence/evaluators/2026-08-09" \
  "$repo_root/docs/operations/evidence/evaluators/2026-08-09/batch-result.json" \
  "$repo_root/docs/operations/evidence/evaluators/2026-08-09/zenith-trial.json" \
  --check
jq -e '.resolutions | length == 2' \
  "$repo_root/docs/operations/evidence/evaluators/2026-08-09/zenith-trial.json" >/dev/null

cp -R "$repo_root/docs/operations/evidence/evaluators/2026-08-09" "$scratch/versioned-store"
mv "$scratch/versioned-store/store" "$scratch/versioned-store/store-v9"
node "$repo_root/scripts/export-zenith-evaluator-trial.mjs" \
  "$scratch/versioned-store" \
  "$scratch/versioned-store/batch-result.json" \
  "$scratch/versioned-store/versioned-trial.json" >/dev/null
jq -e '(.resolutions | length) == 2 and all(.resolutions[]; .record.path | startswith("store-v9/"))' \
  "$scratch/versioned-store/versioned-trial.json" >/dev/null

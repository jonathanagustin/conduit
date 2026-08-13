#!/usr/bin/env bash
set -euo pipefail

if (( $# < 2 )); then
  echo "usage: $0 <git-commit> <evidence-directory> [model ...]" >&2
  exit 2
fi
if [[ -z "${CURSOR_API_KEY:-}" ]]; then
  echo "CURSOR_API_KEY is required" >&2
  exit 2
fi

commit="$1"
evidence_directory="$2"
shift 2
models=("$@")
if (( ${#models[@]} == 0 )); then
  models=(cursor-grok-4.5-high gemini-3.1-pro kimi-k3-max)
fi

repo_root="$(git rev-parse --show-toplevel)"
commit="$(git -C "$repo_root" rev-parse --verify "${commit}^{commit}")"
mkdir -p "$evidence_directory"
evidence_directory="$(cd "$evidence_directory" && pwd)"
"$repo_root/scripts/build-evaluator-snapshot.sh" "$commit" "$evidence_directory/snapshot.json"
cursor_agent_version="$(cursor-agent --version)"
jq -n --arg cursorAgentVersion "$cursor_agent_version" --arg runnerRevision "$commit" \
  '{cursorAgentVersion:$cursorAgentVersion,runnerRevision:$runnerRevision}' > "$evidence_directory/runner.json"

scratch="$(mktemp -d)"
trap 'rm -rf -- "$scratch"' EXIT
git -C "$repo_root" archive --format=tar "$commit" | tar -xf - -C "$scratch"

snapshot_id="$(jq -r .snapshotId "$evidence_directory/snapshot.json")"
cat > "$evidence_directory/prompt.txt" <<EOF
You are an independent, read-only evaluator of the product-agnostic Platform optimization subsystem.

The workspace root is a materialized, byte-exact extraction of immutable Conduit commit $commit and deterministic archive $snapshot_id. Read files directly from this workspace. Do not use git or inspect .git: neither is present or needed. Do not evaluate prior chat claims or a moving working tree. Do not modify files, run network calls, expose credentials, approve changes, or infer that missing evidence means healthy behavior.

Assess correctness, determinism, boundary safety, evidence fidelity, temporal semantics, numerical behavior, cost/usage representation, tests, and documentation for internal/optimization, cmd/platform-optimizer, and their directly referenced docs/scripts. Treat every finding as a hypothesis. Prefer concrete defects reproducible by an executable repository gate. Do not repeat a concern that the extracted code already resolves.

Return one JSON object and no surrounding prose using this exact shape:
{"inputAccessStatus":"COMPLETE|INCOMPLETE","findings":[{"localId":"F1","clusterKey":"stable-semantic-key","category":"correctness|security|provenance|performance|testing|documentation","target":"workspace-relative path:line","summary":"one precise falsifiable claim","confidence":"exact rational in [0,1]","evidence":["specific source observation","specific reproduction command or missing gate"]}],"residualUnknowns":["bounded unknown"]}

Set inputAccessStatus to COMPLETE only if you could read every requested source scope; otherwise set INCOMPLETE and name the inaccessible scope in residualUnknowns. Use unique localId values within your response. Use the same clusterKey for findings describing the same underlying defect. An empty findings array is valid. Never mark a finding verified and never recommend bypassing Platform approval, immutable coordinates, outbound-only Spoke execution, exact reported state, or rollback behavior.
EOF

for model in "${models[@]}"; do
  if [[ ! "$model" =~ ^[A-Za-z0-9._-]+$ ]]; then
    echo "unsafe model identifier: $model" >&2
    exit 2
  fi
  receipt="$evidence_directory/$model.receipt.json"
  timing="$evidence_directory/$model.timing.json"
  started_at="$(date -u +%Y-%m-%dT%H:%M:%S.%3NZ)"
  started_ms="$(date -u +%s%3N)"
  cursor-agent --print --mode ask --sandbox enabled --trust \
    --workspace "$scratch" --model "$model" --output-format json \
    "$(<"$evidence_directory/prompt.txt")" > "$receipt"
  finished_ms="$(date -u +%s%3N)"
  finished_at="$(date -u +%Y-%m-%dT%H:%M:%S.%3NZ)"
  jq -e '.is_error == false and (.result | type == "string") and (.usage.inputTokens | type == "number")' "$receipt" >/dev/null
  jq -n --arg model "$model" --arg startedAt "$started_at" --arg finishedAt "$finished_at" \
    --argjson wallDurationMilliseconds "$((finished_ms - started_ms))" \
    '{model:$model,startedAt:$startedAt,finishedAt:$finishedAt,wallDurationMilliseconds:$wallDurationMilliseconds}' > "$timing"
  echo "completed model=$model receipt=$(sha256sum "$receipt" | awk '{print $1}')"
done

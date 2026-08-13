#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
work_dir=$(mktemp -d)
trap 'rm -rf "$work_dir"' EXIT
selection="$repo_root/api/conjure/foundry-apollo.selection.json"
ir="$repo_root/$(jq -r '.contract.irPath' "$selection")"
go_output="$repo_root/$(jq -r '.contract.goOutput' "$selection")"
go_package=$(jq -r '.contract.goPackage' "$selection")
contract_version=$(jq -r '.contract.version' "$selection")
source_commit=$(jq -r '.source.commit' "$selection")
source_npm_version=$(jq -r '.source.npmVersion' "$selection")
evidence="$repo_root/$(jq -r '.contract.evidencePath' "$selection")"

go run ./internal/tools/conjuregen \
  -input "$ir" \
  -output "$work_dir/$go_package" \
  -package "$go_package"
gofmt -w "$work_dir/$go_package"
diff -ru "$go_output" "$work_dir/$go_package"

jq --exit-status \
  --arg contract_version "$contract_version" \
  --arg source_commit "$source_commit" \
  --arg source_npm_version "$source_npm_version" '
  .version == 1 and
  (.errors | length) == 0 and
  (.services | length) == 0 and
  (.extensions.platform.javaInventory | length) == 42 and
  .extensions.platform.contract.version == $contract_version and
  .extensions.platform.source.commit == $source_commit and
  .extensions.platform.source.npmVersion == $source_npm_version
' "$ir" >/dev/null

expected_ir_hash=$(jq -r '.derived.conjureIrSha256' "$evidence")
actual_ir_hash=$(sha256sum "$ir" | awk '{print $1}')
test "$actual_ir_hash" = "$expected_ir_hash"
jq --exit-status \
  --arg source_commit "$source_commit" \
  --arg source_npm_version "$source_npm_version" \
  '.source.provenanceCommit == $source_commit and .source.npmVersion == $source_npm_version' \
  "$evidence" >/dev/null

echo "Foundry interoperability contract is source-pinned and regeneration-clean"

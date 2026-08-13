#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
selection="${FOUNDRY_INTEROP_SELECTION:-$repo_root/api/conjure/foundry-apollo.selection.json}"
work_dir=$(mktemp -d)
trap 'rm -rf "$work_dir"' EXIT

commit=$(jq -r '.source.commit' "$selection")
source_repository=$(jq -r '.source.repository' "$selection")
typescript_version=$(jq -r '.source.typescriptVersion' "$selection")
git clone --filter=blob:none --no-checkout "$source_repository" "$work_dir/osdk-ts" >/dev/null
git -C "$work_dir/osdk-ts" checkout --detach "$commit" >/dev/null

expected_npm_version=$(jq -r '.source.npmVersion' "$selection")
package_json="$work_dir/osdk-ts/$(jq -r '.source.packageJson' "$selection")"
test "$(jq -r '.version' "$package_json")" = "$expected_npm_version"
evidence="$repo_root/$(jq -r '.contract.evidencePath' "$selection")"
while IFS=$'\t' read -r path expected_hash; do
  actual_hash=$(sha256sum "$work_dir/osdk-ts/$path" | awk '{print $1}')
  test "$actual_hash" = "$expected_hash"
done < <(jq -r '.files[] | [.path, .sha256] | @tsv' "$evidence")

npm install --prefix "$work_dir/tools" --ignore-scripts --no-save "typescript@$typescript_version" >/dev/null
typescript_js="$work_dir/tools/node_modules/typescript/lib/typescript.js"
generated="$work_dir/osdk-ts/$(jq -r '.source.generatedDir' "$selection")"
ir="$repo_root/$(jq -r '.contract.irPath' "$selection")"
go_output="$repo_root/$(jq -r '.contract.goOutput' "$selection")"
go_package=$(jq -r '.contract.goPackage' "$selection")

node "$repo_root/scripts/osdk-conjure-contract.mjs" "$typescript_js" "$generated" "$selection" "$ir"
go run ./internal/tools/conjuregen -input "$ir" -output "$go_output" -package "$go_package"
gofmt -w "$go_output"

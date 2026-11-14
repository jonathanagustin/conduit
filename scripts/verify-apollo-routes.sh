#!/usr/bin/env bash
# Regenerate the complete Apollo public transport boundary from the pinned
# OpenAPI snapshot and require the committed file to match, the same way the
# Foundry interop contract is held to its Conjure IR.
#
# This is what makes the generated table trustworthy: without it the file is
# just Go someone could edit, and "generated" would be a comment rather than a
# property.
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
snapshot="$repo_root/docs/reference-platform/snapshots/reference-openapi-c663874bd7467a01e719d039509b0f206d05fe2c/api-definition.yml"
config="$repo_root/api/apollo-public.codegen.yaml"
generated="$repo_root/internal/apollo/openapi.gen.go"
work_dir=$(mktemp -d)
trap 'rm -rf "$work_dir"' EXIT

test -f "$snapshot"
test -f "$config"
sed "s#^output:.*#output: $work_dir/openapi.gen.go#" "$config" > "$work_dir/codegen.yaml"
(
  cd "$repo_root"
  go tool oapi-codegen --config "$work_dir/codegen.yaml" "$snapshot"
)

diff -u "$generated" "$work_dir/openapi.gen.go"

routes=$(sed -n '/^type StrictServerInterface interface {/,/^}/p' "$generated" | grep -c 'ctx context.Context')
paths=$(ruby -r"$repo_root/scripts/lib/safe_yaml" -e 'puts ConduitSafeYAML.file(ARGV[0]).fetch("paths").sum { |_, item|
  item.keys.count { |method| %w[get post put patch delete].include?(method) } }' "$snapshot")
test "$routes" = "$paths"

models=$(grep -c '^// .* defines model for ' "$generated")
test "$models" -gt 0

echo "Apollo public transport is generated from the pinned snapshot: $routes/$paths operations, $models model declarations"

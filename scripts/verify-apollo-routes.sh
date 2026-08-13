#!/usr/bin/env bash
# Regenerate the Apollo public route table from the pinned OpenAPI snapshot and
# require the committed file to match, the same way the Foundry interop contract
# is held to its Conjure IR.
#
# This is what makes the generated table trustworthy: without it the file is
# just Go someone could edit, and "generated" would be a comment rather than a
# property.
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
snapshot="$repo_root/docs/apollo/snapshots/apollo-openapi-c663874bd7467a01e719d039509b0f206d05fe2c/api-definition.yml"
generated="$repo_root/internal/apollo/routes.gen.go"
work_dir=$(mktemp -d)
trap 'rm -rf "$work_dir"' EXIT

test -f "$snapshot"
go run "$repo_root/internal/tools/apolloroutegen" \
  -input "$snapshot" \
  -output "$work_dir/apollo_routes.gen.go" \
  -package apollo

# The generator records its input path in the header, so compare against a
# regeneration that used the same path the committed file names.
diff -u "$generated" "$work_dir/apollo_routes.gen.go"

routes=$(grep -c '{Operation: ' "$generated")
paths=$(ruby -ryaml -e 'puts YAML.unsafe_load_file(ARGV[0]).fetch("paths").sum { |_, item|
  item.keys.count { |method| %w[get post put patch delete].include?(method) } }' "$snapshot")
test "$routes" = "$paths"

echo "Apollo public route table is generated from the pinned snapshot: $routes/$paths operations"

#!/usr/bin/env bash
# Reproduce the planning wire boundary with the repository's pinned generator.
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
work_dir=$(mktemp -d)
trap 'rm -rf "$work_dir"' EXIT
cd "$root"
sed "s#^output:.*#output: $work_dir/openapi.gen.go#" api/planning-sessions.codegen.yaml > "$work_dir/config.yaml"
go tool oapi-codegen --config "$work_dir/config.yaml" api/planning-sessions.yaml
diff -u internal/planning/transport/openapi.gen.go "$work_dir/openapi.gen.go"
echo 'Planning API generation matches the committed contract boundary'

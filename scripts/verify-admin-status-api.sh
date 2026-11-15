#!/usr/bin/env bash
# Reproduce the read-only administrator wire boundary with the pinned generator.
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
work_dir=$(mktemp -d)
trap 'rm -rf "$work_dir"' EXIT
cd "$root"
sed "s#^output:.*#output: $work_dir/openapi.gen.go#" api/admin-status.codegen.yaml > "$work_dir/config.yaml"
go tool oapi-codegen --config "$work_dir/config.yaml" api/admin-status.json
diff -u internal/adminstatus/transport/openapi.gen.go "$work_dir/openapi.gen.go"
echo 'Administrator status generation matches the committed contract boundary'

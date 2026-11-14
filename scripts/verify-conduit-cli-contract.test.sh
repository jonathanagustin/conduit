#!/usr/bin/env bash
set -euo pipefail
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
stage() {
  rm -rf "$work/tree"
  mkdir -p "$work/tree/docs/reference-platform/snapshots" "$work/tree/scripts"
  cp "$repo_root/scripts/verify-conduit-cli-contract.mjs" "$work/tree/scripts/"
  for file in cli-command-contract.json cli-command-inventory.json public-documentation-manifest.tsv; do
    cp "$repo_root/docs/reference-platform/$file" "$work/tree/docs/reference-platform/"
  done
  cp "$repo_root/docs/reference-platform/snapshots/cli-command-sources-2026-11-14-364.tsv" "$work/tree/docs/reference-platform/snapshots/"
}
run() { (cd "$work/tree" && node scripts/verify-conduit-cli-contract.mjs); }
reject() {
  local expected="$1" output
  if output="$(run 2>&1)"; then
    echo "FAIL accepted $expected" >&2; exit 1
  fi
  [[ "$output" == *"$expected"* ]] || { echo "$output" >&2; exit 1; }
}
stage
run
stage
node -e 'const fs=require("fs"),p=process.argv[1],d=JSON.parse(fs.readFileSync(p)); d.commands[0].sourceSha256="bad";fs.writeFileSync(p,JSON.stringify(d))' "$work/tree/docs/reference-platform/cli-command-contract.json"
reject 'Apollo CLI source hash mismatch'
stage
node -e 'const fs=require("fs"),p=process.argv[1],d=JSON.parse(fs.readFileSync(p)); d.commands[0].sourceSha256="bad";fs.writeFileSync(p,JSON.stringify(d))' "$work/tree/docs/reference-platform/cli-command-inventory.json"
reject 'Apollo CLI current inventory source mismatch'
stage
node -e 'const fs=require("fs"),p=process.argv[1],d=JSON.parse(fs.readFileSync(p)); d.commands.pop();fs.writeFileSync(p,JSON.stringify(d))' "$work/tree/docs/reference-platform/cli-command-inventory.json"
reject 'Apollo CLI current inventory does not cover the live manifest'
stage
node -e 'const fs=require("fs"),p=process.argv[1],d=JSON.parse(fs.readFileSync(p)); d.sourceManifest="docs/reference-platform/public-documentation-manifest.tsv";fs.writeFileSync(p,JSON.stringify(d))' "$work/tree/docs/reference-platform/cli-command-contract.json"
reject 'Apollo CLI compatibility profile must name its reviewed source snapshot'
echo 'PASS CLI evidence gate: profile and current inventory reject independent drift'


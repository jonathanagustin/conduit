#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${root}"
node tools/workspace-schemas/verify.mjs
node --test tools/workspace-schemas/verify.test.mjs
output="$(mktemp)"
trap 'rm -f "${output}"' EXIT
go run ./tools/workspace-schemas/generate "${output}"
cmp workspace-schemas/v1/plan.schema.json "${output}"
npm ci --prefix tools/workspace-schemas --ignore-scripts --no-audit --no-fund
# Official Node 26.13.1 bookworm-slim, Linux/amd64 manifest (not a mutable tag).
image='node@sha256:18a166e2d50578afe933434857ed2d9059bd1aa398a223e9c89eb4302354f079'
docker run --rm --network none --read-only --cap-drop ALL \
  --security-opt no-new-privileges --user 65534:65534 \
  --mount "type=bind,src=${root}/workspace-schemas,dst=/opt/conduit/workspace-schemas,readonly" \
  --mount "type=bind,src=${root}/tools/workspace-schemas,dst=/verification,readonly" \
  --workdir /verification "${image}" node diagnostics.mjs

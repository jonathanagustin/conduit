#!/usr/bin/env bash
set -euo pipefail
umask 022
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${root}"
cmp examples/example-foundation.json workspace-schemas/v1/fixtures/valid.foundation.plan.yaml
node tools/workspace-schemas/verify.mjs
node --test tools/workspace-schemas/verify.test.mjs
mkdir -p "${root}/.cache"
staging="$(mktemp -d "${root}/.cache/workspace-schemas.XXXXXXXX")"
trap 'rm -rf -- "${staging}"' EXIT
output="${staging}/generated-plan.json"
go run ./tools/workspace-schemas/generate "${output}"
cmp workspace-schemas/v1/plan.schema.json "${output}"
npm ci --prefix tools/workspace-schemas --ignore-scripts --no-audit --no-fund
# Git does not record read modes. A checkout made with umask 077 must still
# verify as the container's unrelated, unprivileged UID. Copy only public test
# materials and locked dependencies, and normalize the copies, never the tree.
mkdir -p "${staging}/pack" "${staging}/verification"
cp -R workspace-schemas/. "${staging}/pack/"
cp tools/workspace-schemas/diagnostics.mjs tools/workspace-schemas/package.json "${staging}/verification/"
cp -R tools/workspace-schemas/node_modules "${staging}/verification/node_modules"
find "${staging}" -type d -exec chmod 0755 {} +
find "${staging}" -type f -exec chmod 0644 {} +
# Official Node 26.13.1 bookworm-slim, Linux/amd64 manifest (not a mutable tag).
image='node@sha256:18a166e2d50578afe933434857ed2d9059bd1aa398a223e9c89eb4302354f079'
docker run --rm --network none --read-only --cap-drop ALL \
  --security-opt no-new-privileges --user 65534:65534 \
  --mount "type=bind,src=${staging}/pack,dst=/opt/conduit/workspace-schemas,readonly" \
  --mount "type=bind,src=${staging}/verification,dst=/verification,readonly" \
  --workdir /verification "${image}" node diagnostics.mjs

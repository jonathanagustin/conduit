#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
hadolint_bin="${HADOLINT_BIN:-hadolint}"

# Every Containerfile in the repository, not a hand-maintained pair.
#
# The list was `Containerfile` and `Containerfile.agent` only, so the two
# optimizer Containerfiles — the largest and the ones doing the most unusual
# build work — were never linted, and adding a violation to either left this
# gate green. Both pass today, so widening the gate costs nothing and closes
# the hole. Globbed rather than listed: a Containerfile added tomorrow is
# covered without anyone remembering to add it here.
shopt -s nullglob
containerfiles=("${repo_root}"/Containerfile*)
shopt -u nullglob
if [[ "${#containerfiles[@]}" -eq 0 ]]; then
  echo "no Containerfiles found in ${repo_root}; the glob no longer matches what it guards" >&2
  exit 1
fi

"${hadolint_bin}" --failure-threshold info "${containerfiles[@]}"

# Every input directory hashed and validated by scripts/build-console.sh must be
# both unignored in .dockerignore and copied into Containerfile's console stage,
# otherwise Hub image builds fail only when Containerfile is built (Spoke E2E /
# OCI images) while Trunk's non-container console build stays green.
node --input-type=module - "${repo_root}" <<'JS'
import { readFileSync } from 'node:fs';
import { join } from 'node:path';

const root = process.argv[2];
const buildScript = readFileSync(join(root, 'scripts/build-console.sh'), 'utf8');
const match = buildScript.match(/for\s*\(\s*const\s+directory\s+of\s*\[([^\]]+)\]\s*\)\s*walk\(directory\)/);
if (!match) {
  console.error('verify-containerfiles: cannot locate console input directory list in scripts/build-console.sh');
  process.exit(1);
}
const dirs = Array.from(match[1].matchAll(/['"]([^'"]+)['"]/g), (m) => m[1]);
if (dirs.length === 0) {
  console.error('verify-containerfiles: scripts/build-console.sh declared zero console input directories');
  process.exit(1);
}
const containerfile = readFileSync(join(root, 'Containerfile'), 'utf8');
const dockerignore = readFileSync(join(root, '.dockerignore'), 'utf8');
const errors = [];
for (const dir of dirs) {
  if (!new RegExp(`^COPY\\s+${dir.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}/?\\s+${dir.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}/?$`, 'm').test(containerfile)) {
    errors.push(`Containerfile missing COPY for console input directory: ${dir}/`);
  }
  if (!dockerignore.includes(`!${dir}/`) || !dockerignore.includes(`!${dir}/**`)) {
    errors.push(`.dockerignore missing allowlist (!${dir}/ and !${dir}/**) for console input directory: ${dir}`);
  }
}
if (errors.length > 0) {
  for (const err of errors) console.error(`verify-containerfiles: ${err}`);
  process.exit(1);
}
console.log(`Containerfile and .dockerignore cover all ${dirs.length} console build input directories (${dirs.join(', ')})`);
JS

# Every top-level module directory imported by the binaries built in
# Containerfile must be copied into its Go build stage; otherwise `go build`
# fails only when the Hub image is built (Spoke E2E / OCI images).
if command -v go >/dev/null 2>&1; then
  missing_go_dirs=()
  while IFS= read -r top_dir; do
    [[ -z "${top_dir}" ]] && continue
    if ! grep -Eq "^COPY[[:space:]]+${top_dir}/?[[:space:]]+${top_dir}/?$" "${repo_root}/Containerfile"; then
      missing_go_dirs+=("${top_dir}")
    fi
  done < <(
    go list -deps ./cmd/platform-delivery ./cmd/platform-module-publisher ./cmd/platform-grype-scanner ./cmd/platform-operator \
      | grep '^github\.com/jonathanagustin/conduit/' \
      | sed 's|^github\.com/jonathanagustin/conduit/||' \
      | cut -d/ -f1 \
      | sort -u
  )
  if [[ "${#missing_go_dirs[@]}" -gt 0 ]]; then
    echo "verify-containerfiles: Containerfile Go build stage missing COPY for imported directories: ${missing_go_dirs[*]}" >&2
    exit 1
  fi
fi

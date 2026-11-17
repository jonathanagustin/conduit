#!/usr/bin/env bash
# Build the operator console into internal/consoleui/dist.
#
# Node and pnpm are the versions pinned in mise.toml. When those exact
# binaries are already on PATH, they are used. Otherwise the build runs
# through mise. The install is `pnpm install --frozen-lockfile`, the locked
# install for this tree (it has a pnpm-lock.yaml, not a package-lock.json).
# A second run whose inputs have not changed does no work.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${root}"

read_pin() {
  local key="$1"
  local value
  value="$(awk -F'"' -v key="${key}" '$1 ~ "^" key " = " { print $2; exit }' "${root}/mise.toml")"
  if [[ -z "${value}" ]]; then
    echo "mise.toml has no ${key} pin" >&2
    exit 1
  fi
  printf '%s' "${value}"
}

node_pin="$(read_pin node)"
pnpm_pin="$(read_pin pnpm)"

run_pinned() {
  if [[ "$(node -v 2>/dev/null || true)" == "v${node_pin}" && "$(pnpm -v 2>/dev/null || true)" == "${pnpm_pin}" ]]; then
    "$@"
    return
  fi
  if ! command -v mise >/dev/null 2>&1; then
    echo "need node ${node_pin} and pnpm ${pnpm_pin}; install them with mise" >&2
    exit 1
  fi
  mise exec -- "$@"
}

# A second output directory is a clean rebuild for the asset diff. It must not
# update the embedded tree or the up-to-date stamp.
if [[ -n "${CONSOLE_BUILD_OUT:-}" ]]; then
  run_pinned pnpm --dir "${root}/web" install --frozen-lockfile
  run_pinned node "${root}/tools/a2ui/generate-validator.mjs" --check
  run_pinned node "${root}/tools/liveevents/generate-validator.mjs" --check
  PLATFORM_CONSOLE_OUT_DIR="${CONSOLE_BUILD_OUT}" run_pinned pnpm --dir "${root}/web" build
  exit 0
fi

hash_inputs() {
  run_pinned node --input-type=module - "${root}" <<'JS'
import { createHash } from 'node:crypto';
import { readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
const root = process.argv[2];
const excluded = new Set(['node_modules', 'dist', '.stryker-tmp', 'test-results', 'playwright-report', 'coverage'].map(name => 'web/' + name));
const files = ['scripts/build-console.sh', 'mise.toml'];
const walk = directory => {
  for (const entry of readdirSync(join(root, directory), { withFileTypes: true })) {
    const relative = directory + '/' + entry.name;
    if (excluded.has(relative)) continue;
    if (entry.isDirectory()) walk(relative);
    else if (entry.isFile()) files.push(relative);
  }
};
for (const directory of ['web', 'api/a2ui', 'tools/a2ui', 'api/live-events', 'tools/liveevents']) walk(directory);
files.sort((left, right) => Buffer.compare(Buffer.from(left), Buffer.from(right)));
const hash = createHash('sha256');
for (const file of files) {
  const digest = createHash('sha256').update(readFileSync(join(root, file))).digest('hex');
  hash.update(file).update('\0').update(digest).update('\n');
}
process.stdout.write(hash.digest('hex') + '\n');
JS
}

dist="${root}/internal/consoleui/dist"
anchor="${dist}/README.txt"
stamp="${dist}/.build-stamp"
index="${dist}/index.html"
current="$(hash_inputs)"

if [[ -f "${stamp}" && -f "${index}" ]] \
  && grep -q 'id="root"' "${index}" \
  && grep -q 'conduit-console-bootstrap' "${index}" \
  && [[ "$(cat "${stamp}")" == "${current}" ]]; then
  echo "console build is up to date"
  exit 0
fi

if [[ ! -f "${anchor}" ]]; then
  echo "missing ${anchor}" >&2
  exit 1
fi

saved="$(mktemp)"
cp "${anchor}" "${saved}"
restore_anchor() {
  cp "${saved}" "${anchor}"
  rm -f "${saved}"
}
trap restore_anchor EXIT

run_pinned pnpm --dir "${root}/web" install --frozen-lockfile
run_pinned node "${root}/tools/a2ui/generate-validator.mjs" --check
run_pinned node "${root}/tools/liveevents/generate-validator.mjs" --check
run_pinned pnpm --dir "${root}/web" build
printf '%s\n' "${current}" >"${stamp}"
echo "built the operator console into internal/consoleui/dist"

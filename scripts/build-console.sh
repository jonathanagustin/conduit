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
  {
    find "${root}/web" \
      \( -path "${root}/web/node_modules" -o -path "${root}/web/dist" \
         -o -path "${root}/web/.stryker-tmp" -o -path "${root}/web/test-results" \
         -o -path "${root}/web/playwright-report" -o -path "${root}/web/coverage" \
      \) -prune -o -type f -print
    find "${root}/api/a2ui" "${root}/tools/a2ui" "${root}/api/live-events" "${root}/tools/liveevents" -type f -print
    printf '%s\n' "${root}/scripts/build-console.sh" "${root}/mise.toml"
  } | LC_ALL=C sort | xargs -d '\n' sha256sum | sha256sum | awk '{print $1}'
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

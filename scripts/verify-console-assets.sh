#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
"${repo_root}/scripts/build-console.sh"

generated="$(mktemp -d)"
trap 'rm -rf "${generated}"' EXIT

CONSOLE_BUILD_OUT="${generated}" "${repo_root}/scripts/build-console.sh"

if ! diff --recursive --brief \
  --exclude README.txt \
  --exclude .build-stamp \
  "${repo_root}/internal/consoleui/dist" "${generated}"; then
  echo "embedded console assets differ from a clean production build" >&2
  echo "run: scripts/build-console.sh" >&2
  exit 1
fi

echo "embedded console assets match the production build"
